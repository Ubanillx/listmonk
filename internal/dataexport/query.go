// Package dataexport implements administrator-only, workspace-scoped exports.
package dataexport

import (
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

var Types = map[string]string{
	"campaigns": "营销活动汇总", "activity": "营销行为明细", "customers": "客户名单",
	"blocklist": "黑名单", "bounces": "退信投诉明细", "bounce_customers": "退信客户名单",
	"lists": "列表目录", "pools": "公海分配名单",
	"pool_contacts": "一级公海联系人",
}

type Request struct {
	Type               string     `json:"type"`
	Format             string     `json:"format"`
	Search             string     `json:"search"`
	Query              string     `json:"query"`
	ListIDs            []int64    `json:"list_ids"`
	IDs                []int64    `json:"ids"`
	CampaignIDs        []int64    `json:"campaign_ids"`
	Status             string     `json:"status"`
	SubscriptionStatus string     `json:"subscription_status"`
	Source             string     `json:"source"`
	BounceType         string     `json:"bounce_type"`
	From               *time.Time `json:"from"`
	To                 *time.Time `json:"to"`
}

func (r Request) Validate() error {
	if Types[r.Type] == "" || (r.Format != "csv" && r.Format != "xlsx") {
		return fmt.Errorf("请选择有效的导出类型和格式")
	}
	if len(r.Search) > 500 || len(r.Source) > 100 || len(r.IDs)+len(r.ListIDs)+len(r.CampaignIDs) > 10000 {
		return fmt.Errorf("筛选条件过长")
	}
	for _, ids := range [][]int64{r.IDs, r.ListIDs, r.CampaignIDs} {
		for _, id := range ids {
			if id < 1 {
				return fmt.Errorf("无效的筛选编号")
			}
		}
	}
	if r.From != nil && r.To != nil && r.From.After(*r.To) {
		return fmt.Errorf("开始时间不能晚于结束时间")
	}
	if r.Status != "" && !strings.Contains("|enabled|disabled|blocklisted|active|archived|draft|running|scheduled|paused|deferred|cancelled|finished|", "|"+r.Status+"|") {
		return fmt.Errorf("无效状态")
	}
	if r.SubscriptionStatus != "" && r.SubscriptionStatus != "confirmed" && r.SubscriptionStatus != "unconfirmed" && r.SubscriptionStatus != "unsubscribed" {
		return fmt.Errorf("无效订阅状态")
	}
	if r.BounceType != "" && r.BounceType != "soft" && r.BounceType != "hard" && r.BounceType != "complaint" {
		return fmt.Errorf("无效退信类型")
	}
	return nil
}

type Access struct {
	UserID, OrganizationID                              int
	PlatformAdmin, IndividualTracking, TrackingDisabled bool
}

type query struct{ args []any }

func (q *query) arg(v any) string { q.args = append(q.args, v); return fmt.Sprintf("$%d", len(q.args)) }
func (q *query) ids(col string, ids []int64) string {
	if len(ids) == 0 {
		return "TRUE"
	}
	return col + " = ANY(" + q.arg(pq.Array(ids)) + "::bigint[])"
}
func (q *query) search(cols []string, s string) string {
	if s == "" {
		return "TRUE"
	}
	p := q.arg("%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(s) + "%")
	x := make([]string, len(cols))
	for i, c := range cols {
		x[i] = c + " ILIKE " + p
	}
	return "(" + strings.Join(x, " OR ") + ")"
}
func (q *query) dates(col string, r Request) string {
	s := "TRUE"
	if r.From != nil {
		s += " AND " + col + ">=" + q.arg(*r.From)
	}
	if r.To != nil {
		s += " AND " + col + "<=" + q.arg(*r.To)
	}
	return s
}

func maskedEmail(email string) string {
	return "CASE WHEN " + email + "='' THEN '' WHEN position('@' in " + email + ")=0 THEN '***' ELSE CASE WHEN length(split_part(" + email + ",'@',1))>3 THEN left(" + email + ",3) ELSE '' END || '***@' || split_part(" + email + ",'@',2) END"
}

// BuildQuery always scopes both customer and list relations. A platform admin
// selects an organization explicitly; organization_id=0 means personal data.
func BuildQuery(r Request, a Access) (string, []any, error) {
	if err := r.Validate(); err != nil {
		return "", nil, err
	}
	q := &query{}
	org := q.arg(a.OrganizationID)
	scope := func(alias string) string { return "COALESCE(" + alias + ".organization_id,0)=" + org }
	email := func(expr, mask string) string {
		if a.PlatformAdmin {
			return expr
		}
		return "CASE WHEN " + mask + " THEN " + maskedEmail(expr) + " ELSE " + expr + " END"
	}
	listMask := "EXISTS(SELECT 1 FROM customer_list_memberships mm JOIN customer_lists ml ON ml.id=mm.customer_list_id WHERE mm.customer_id=c.id AND " + scope("ml") + " AND ml.mask_emails)"
	ce := email("c.email", listMask)
	stmt := ""
	switch r.Type {
	case "customers", "blocklist":
		condition, err := q.expression(r.Query)
		if err != nil {
			return "", nil, err
		}
		where := scope("c") + " AND " + q.ids("c.id", r.IDs) + " AND " + q.search([]string{"c.email", "c.name", "c.customer_code"}, r.Search)
		where += " AND " + condition
		if r.Type == "blocklist" {
			where += " AND c.status='blocklisted'"
		} else if r.Status != "" {
			where += " AND c.status::text=" + q.arg(r.Status)
		}
		mf := scope("l") + " AND " + q.ids("l.id", r.ListIDs)
		if r.SubscriptionStatus != "" {
			mf += " AND m.status::text=" + q.arg(r.SubscriptionStatus)
		}
		if len(r.ListIDs) > 0 || r.SubscriptionStatus != "" {
			where += " AND EXISTS(SELECT 1 FROM customer_list_memberships m JOIN customer_lists l ON l.id=m.customer_list_id WHERE m.customer_id=c.id AND " + mf + ")"
		}
		where += " AND " + q.dates("c.created_at", r)
		stmt = `SELECT c.id AS "客户ID", c.customer_code AS "客户编码", c.name AS "公司或姓名", ` + ce + ` AS "邮箱",
 CASE WHEN ` + listMask + ` AND ` + q.arg(!a.PlatformAdmin) + ` THEN '{}'::jsonb ELSE c.attribs END AS "自定义字段",
 c.status AS "客户状态", (SELECT string_agg(l.name || ' [' || m.status || ']', '; ' ORDER BY l.id)
 FROM customer_list_memberships m JOIN customer_lists l ON l.id=m.customer_list_id WHERE m.customer_id=c.id AND ` + mf + `) AS "所属列表及订阅状态",
 c.created_at AS "创建时间",c.updated_at AS "更新时间" FROM customers c WHERE ` + where + ` ORDER BY c.id`
	case "lists":
		w := scope("l") + " AND " + q.ids("l.id", r.ListIDs) + " AND " + q.ids("l.id", r.IDs) + " AND " + q.search([]string{"l.name"}, r.Search)
		if r.Status != "" {
			w += " AND l.status::text=" + q.arg(r.Status)
		}
		stmt = `SELECT l.id AS "列表ID", l.name AS "列表名称",l.type AS "列表类型",l.status AS "状态", l.mask_emails AS "邮箱打码",
 u.name AS "所有者", l.created_at AS "创建时间" FROM customer_lists l LEFT JOIN users u ON u.id=l.owner_user_id WHERE ` + w + ` ORDER BY l.id`
	case "pools":
		w := scope("s") + " AND " + q.search([]string{"pc.customer_code", "pc.company_name"}, r.Search) + " AND " + q.ids("pc.id", r.IDs)
		w += " AND (" + q.ids("s.list_id", r.ListIDs) + " OR " + q.ids("s.pool_id", r.ListIDs) + ")"
		joins := ` FROM pool_segments s JOIN customer_lists p ON p.id=s.pool_id JOIN customer_lists sl ON sl.id=s.list_id
 JOIN pool_segment_members sm ON sm.segment_id=s.id JOIN pool_contacts pc ON pc.id=sm.contact_id
 LEFT JOIN pool_segment_exclusions e ON e.pool_id=s.pool_id AND e.organization_id=s.organization_id AND e.contact_id=pc.id`
		stmt = `SELECT pc.id AS "公海联系人ID",pc.customer_code AS "客户编码",pc.company_name AS "公司名称",` + email("pc.email", "TRUE") + ` AS "邮箱",
 p.name AS "一级公海",sl.name AS "组织二级列表", CASE WHEN sm.status='removed' OR (e.pool_id IS NOT NULL AND e.restored_at IS NULL) THEN '已移除' ELSE '已分配' END AS "分配状态",
 CASE WHEN sm.status='removed' THEN COALESCE(NULLIF(sm.removed_reason,''),CASE WHEN e.restored_at IS NULL THEN e.reason END,'')
 WHEN e.pool_id IS NOT NULL AND e.restored_at IS NULL THEN e.reason ELSE '' END AS "移除原因",
 sm.created_at AS "分配时间"` + joins + ` WHERE ` + w + ` ORDER BY pc.id,s.id`
	case "pool_contacts":
		if !a.PlatformAdmin {
			return "", nil, fmt.Errorf("一级公海主数据仅最高管理员可导出")
		}
		stmt = `SELECT pc.id AS "公海联系人ID",pc.customer_code AS "客户编码",pc.company_name AS "公司名称",pc.email AS "邮箱",p.name AS "一级公海",pc.status AS "状态"
 FROM pool_members pm JOIN pool_contacts pc ON pc.id=pm.contact_id JOIN customer_lists p ON p.id=pm.pool_id WHERE ` + org + `::bigint>=0 AND ` + q.ids("p.id", r.ListIDs) + ` AND ` + q.ids("pc.id", r.IDs) + ` AND ` + q.search([]string{"pc.customer_code", "pc.company_name"}, r.Search) + ` ORDER BY pc.id,p.id`
	case "bounces", "bounce_customers":
		w := `((b.pool_contact_id IS NOT NULL AND COALESCE(b.source_organization_id,0)=` + org + `) OR (b.pool_contact_id IS NULL AND ` + scope("c") + ` AND c.id IS NOT NULL))`
		w += " AND " + q.ids("b.campaign_id", r.CampaignIDs) + " AND " + q.ids("b.id", r.IDs) + " AND " + q.dates("b.created_at", r)
		w += " AND " + q.search([]string{"c.customer_code", "c.name", "pc.customer_code", "pc.company_name"}, r.Search)
		if r.Source != "" {
			w += " AND b.source=" + q.arg(r.Source)
		}
		if r.BounceType != "" {
			w += " AND b.type::text=" + q.arg(r.BounceType)
		}
		if len(r.ListIDs) > 0 {
			w += " AND (" + q.ids("s.list_id", r.ListIDs) + " OR EXISTS(SELECT 1 FROM customer_list_memberships mm JOIN customer_lists ml ON ml.id=mm.customer_list_id WHERE mm.customer_id=c.id AND " + scope("ml") + " AND " + q.ids("ml.id", r.ListIDs) + "))"
		}
		base := `SELECT b.id,b.customer_id,b.pool_contact_id,b.source_organization_id,COALESCE(pc.customer_code,c.customer_code) AS code,
 COALESCE(pc.company_name,c.name) AS name,CASE WHEN b.pool_contact_id IS NOT NULL THEN ` + email("pc.email", "TRUE") + ` ELSE ` + ce + ` END AS email,
 ca.name AS campaign,b.type,b.source,b.created_at,p.name AS pool,sl.name AS segment,
 ` + q.arg(a.PlatformAdmin) + `::boolean AS is_admin,b.meta,
 row_number() OVER(PARTITION BY b.customer_id,b.pool_contact_id,b.source_organization_id ORDER BY b.created_at DESC,b.id DESC) AS rn,
 count(*) OVER(PARTITION BY b.customer_id,b.pool_contact_id,b.source_organization_id) AS total
 FROM bounces b LEFT JOIN customers c ON c.id=b.customer_id AND ` + scope("c") + `
 LEFT JOIN pool_contacts pc ON pc.id=b.pool_contact_id
 LEFT JOIN campaigns ca ON ca.id=b.campaign_id AND ` + scope("ca") + `
 LEFT JOIN pool_segments s ON s.id=b.source_segment_id AND ` + scope("s") + `
 LEFT JOIN customer_lists p ON p.id=s.pool_id LEFT JOIN customer_lists sl ON sl.id=s.list_id WHERE ` + w
		stmt = `WITH data AS (` + base + `) SELECT id AS "退信ID",code AS "客户编码",name AS "公司或姓名",email AS "邮箱",campaign AS "营销活动",
 type AS "类型",source AS "来源",CASE WHEN is_admin THEN meta::text ELSE '详细诊断仅最高管理员可导出' END AS "诊断信息",
 created_at AS "退信时间",pool AS "一级公海",segment AS "二级列表",total AS "筛选期间累计次数" FROM data`
		if r.Type == "bounce_customers" {
			stmt += " WHERE rn=1"
		}
		stmt += " ORDER BY id"
	case "campaigns", "activity":
		w := scope("ca") + " AND " + q.ids("ca.id", r.CampaignIDs)
		if r.Type == "campaigns" {
			w += " AND " + q.ids("ca.id", r.IDs) + " AND " + q.search([]string{"ca.name"}, r.Search)
			if r.Status != "" {
				w += " AND ca.status::text=" + q.arg(r.Status)
			}
		}
		if len(r.ListIDs) > 0 {
			w += " AND EXISTS(SELECT 1 FROM campaign_customer_lists cl WHERE cl.campaign_id=ca.id AND " + q.ids("cl.customer_list_id", r.ListIDs) + ")"
		}
		sent := `SELECT cr.campaign_id, cr.customer_id, cr.sent_at AS at FROM campaign_recipients cr WHERE cr.sent_at IS NOT NULL`
		// Pool delivery has no immutable sent_at in the existing schema. Keep it
		// separate in the report, rather than inventing an event timestamp.
		if r.Type == "activity" {
			if !a.IndividualTracking || a.TrackingDisabled {
				return "", nil, fmt.Errorf("未启用个人追踪，不能导出个人营销行为明细")
			}
			stmt = `WITH events AS (
 SELECT id::text,'打开'::text AS kind,campaign_id,customer_id,created_at AS at,NULL::int AS link_id FROM campaign_views
 UNION ALL SELECT id::text,'点击',campaign_id,customer_id,created_at,link_id FROM link_clicks
 UNION ALL SELECT customer_id::text,'发送',campaign_id,customer_id,at,NULL::int FROM (` + sent + `) sent
 ) SELECT e.id AS "事件ID",ca.id AS "活动ID",ca.name AS "营销活动",c.customer_code AS "客户编码",c.name AS "公司或姓名",` + ce + ` AS "邮箱",
 e.kind AS "行为",e.at AS "发生时间",ln.url AS "点击链接" FROM events e JOIN campaigns ca ON ca.id=e.campaign_id
 LEFT JOIN customers c ON c.id=e.customer_id AND ` + scope("c") + ` LEFT JOIN links ln ON ln.id=e.link_id WHERE ` + w + ` AND ` + q.dates("e.at", r) + ` AND ` + q.search([]string{"c.name", "c.customer_code", "c.email"}, r.Search) + ` ORDER BY e.at,e.kind,e.id`
		} else {
			stmt = `WITH scoped AS (SELECT ca.* FROM campaigns ca WHERE ` + w + `),
 sent AS (SELECT x.campaign_id,count(*) n FROM (` + sent + `) x JOIN scoped ca ON ca.id=x.campaign_id WHERE ` + q.dates("x.at", r) + ` GROUP BY x.campaign_id),
 views AS (SELECT e.campaign_id,count(*) n,count(DISTINCT e.customer_id) u FROM campaign_views e JOIN scoped ca ON ca.id=e.campaign_id WHERE ` + q.dates("e.created_at", r) + ` GROUP BY e.campaign_id),
 clicks AS (SELECT e.campaign_id,count(*) n,count(DISTINCT e.customer_id) u FROM link_clicks e JOIN scoped ca ON ca.id=e.campaign_id WHERE ` + q.dates("e.created_at", r) + ` GROUP BY e.campaign_id),
 bnc AS (SELECT e.campaign_id,count(*) FILTER(WHERE e.type<>'complaint') n,count(*) FILTER(WHERE e.type='complaint') complaints
 FROM bounces e JOIN scoped ca ON ca.id=e.campaign_id WHERE ` + q.dates("e.created_at", r) + ` GROUP BY e.campaign_id)
 SELECT ca.id AS "活动ID",ca.name AS "营销活动",ca.status AS "活动状态",ca.started_at AS "开始发送时间",COALESCE(sent.n,0) AS "期间私域发送量",
 (SELECT count(*) FROM campaign_pool_recipients pr WHERE pr.campaign_id=ca.id AND pr.status='sent') AS "公海累计已发送量",
 `
			if a.TrackingDisabled {
				stmt += `NULL::bigint AS "打开次数",NULL::bigint AS "点击次数",`
			} else {
				stmt += `COALESCE(views.n,0) AS "打开次数",COALESCE(clicks.n,0) AS "点击次数",`
			}
			if a.IndividualTracking && !a.TrackingDisabled {
				stmt += `COALESCE(views.u,0) AS "打开人数",COALESCE(clicks.u,0) AS "点击人数",
 round(100.0*COALESCE(views.u,0)/NULLIF(sent.n,0),2) AS "私域打开率百分比",round(100.0*COALESCE(clicks.u,0)/NULLIF(sent.n,0),2) AS "私域点击率百分比",`
			} else {
				stmt += `NULL::bigint AS "打开人数",NULL::bigint AS "点击人数",NULL::numeric AS "私域打开率百分比",NULL::numeric AS "私域点击率百分比",`
			}
			stmt += `COALESCE(bnc.n,0) AS "退信次数",COALESCE(bnc.complaints,0) AS "投诉次数",NULL::bigint AS "退订次数",
 '发送为系统投递记录，不代表进入收件箱；公海发送仅有累计状态；退订无完整活动事件，留空；比例分母为期间私域发送量' AS "统计说明"
 FROM scoped ca LEFT JOIN sent ON sent.campaign_id=ca.id LEFT JOIN views ON views.campaign_id=ca.id LEFT JOIN clicks ON clicks.campaign_id=ca.id LEFT JOIN bnc ON bnc.campaign_id=ca.id ORDER BY ca.id`
		}
	}
	return stmt, q.args, nil
}
