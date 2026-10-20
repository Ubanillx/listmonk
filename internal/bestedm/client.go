// Package bestedm adapts the five documented BestEDM account and reporting APIs.
package bestedm

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const BaseURL = "https://www.bestedm.net/mm-ms/apinew/"
const maxResponse = 4 << 20

type Credentials struct {
	Username string
	Password string
}
type Row map[string]string
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

type Client struct {
	baseURL string
	http    *http.Client
}

func New() *Client {
	return &Client{BaseURL, &http.Client{Timeout: 20 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

type node struct {
	XMLName  xml.Name
	Text     string `xml:",chardata"`
	Children []node `xml:",any"`
}

func (n node) row() Row {
	r := Row{}
	for _, c := range n.Children {
		r[c.XMLName.Local] = strings.TrimSpace(c.Text)
	}
	return r
}

var safeCode = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// errNoData marks a query that the provider answered successfully with no rows.
// It is not a failure: callers map it to an empty result set.
var errNoData = &Error{"no_data", "服务商没有该条件下的记录"}

func upstreamError(code string) error {
	if !safeCode.MatchString(code) {
		code = "provider_error"
	}
	return &Error{code, "服务商拒绝了请求，请检查账号权限、凭据和查询条件"}
}

func parseXML(b []byte) (node, error) {
	var n node
	dec := xml.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(&n); err != nil {
		return n, &Error{"invalid_xml", "服务商返回的 XML 无法解析，请联系平台核对响应格式"}
	}
	for {
		t, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return n, &Error{"invalid_xml", "服务商返回了无效 XML"}
		}
		if _, ok := t.(xml.StartElement); ok {
			return n, &Error{"invalid_xml", "服务商返回了多个 XML 根节点"}
		}
	}
	if n.XMLName.Local == "result" || n.XMLName.Local == "error" {
		status := n.row()["status"]
		if status == "no_data" {
			// An empty result set is a successful answer, not a provider error.
			return n, errNoData
		}
		if status == "" {
			status = "provider_error"
		}
		return n, upstreamError(status)
	}
	return n, nil
}

func (c *Client) get(ctx context.Context, cred Credentials, path string, q url.Values) ([]byte, error) {
	u := c.baseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, &Error{"request_error", "无法建立服务商请求"}
	}
	req.SetBasicAuth(cred.Username, cred.Password)
	req.Header.Set("Accept", "application/xml, application/json, text/xml")
	res, err := c.http.Do(req)
	if err != nil {
		return nil, &Error{"connection_error", "无法连接服务商或请求超时，请稍后重试"}
	}
	defer res.Body.Close()
	if res.StatusCode == 401 || res.StatusCode == 403 {
		return nil, &Error{"authentication_failed", "服务商认证失败，请检查账号密码、服务状态和访问权限"}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, &Error{"upstream_http_error", fmt.Sprintf("服务商暂不可用（HTTP %d）", res.StatusCode)}
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, maxResponse+1))
	if err != nil {
		return nil, &Error{"response_error", "读取服务商响应失败"}
	}
	if len(b) > maxResponse {
		return nil, &Error{"response_too_large", "查询结果超过 4 MB，请缩小日期范围"}
	}
	b = bytes.TrimSpace(bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf}))
	if len(b) == 0 {
		return nil, &Error{"empty_response", "服务商返回空响应，无法判断查询结果"}
	}
	return b, nil
}

func (c *Client) account(ctx context.Context, cred Credentials, action, root string) (Row, error) {
	b, err := c.get(ctx, cred, "account.php", url.Values{"do": {action}})
	if err != nil {
		return nil, err
	}
	n, err := parseXML(b)
	if err != nil {
		return nil, err
	}
	if n.XMLName.Local != root {
		return nil, &Error{"unexpected_response", "服务商返回了非预期的账号数据"}
	}
	r := n.row()
	if len(r) == 0 {
		return nil, &Error{"unexpected_response", "服务商未返回账号字段"}
	}
	if root == "customer" && (r["id"] == "" || r["username"] == "") {
		return nil, &Error{"authentication_failed", "未能取得有效账号资料，请检查账号密码"}
	}
	return r, nil
}
func (c *Client) Customer(ctx context.Context, cred Credentials) (Row, error) {
	return c.account(ctx, cred, "customer-info", "customer")
}
func (c *Client) Service(ctx context.Context, cred Credentials) (Row, error) {
	return c.account(ctx, cred, "service-info", "service")
}

func (c *Client) Statistics(ctx context.Context, cred Credentials, q url.Values) ([]Row, error) {
	b, err := c.get(ctx, cred, "stask.php", q)
	if err != nil {
		return nil, err
	}
	n, err := parseXML(b)
	if err != nil {
		if errors.Is(err, errNoData) {
			return []Row{}, nil
		}
		return nil, err
	}
	if n.XMLName.Local != "data" {
		return nil, &Error{"unexpected_response", "发送统计返回结构不符合文档"}
	}
	rows := []Row{}
	for _, child := range n.Children {
		if child.XMLName.Local == "task" {
			rows = append(rows, child.row())
		}
	}
	return rows, nil
}

func (c *Client) Failures(ctx context.Context, cred Credentials, q url.Values) ([]Row, error) {
	copied := url.Values{}
	for key, values := range q {
		copied[key] = append([]string(nil), values...)
	}
	q = copied
	q.Set("out_type", "xml")
	b, err := c.get(ctx, cred, "failexport.php", q)
	if err != nil {
		return nil, err
	}
	n, err := parseXML(b)
	if err != nil {
		if errors.Is(err, errNoData) {
			return []Row{}, nil
		}
		return nil, err
	}
	if n.XMLName.Local != "fail_recipient_list" {
		if errors.Is(err, errNoData) {
			return []Row{}, nil
		}
		return nil, &Error{"unexpected_response", "失败地址返回结构不符合文档"}
	}
	rows := []Row{}
	for _, child := range n.Children {
		if child.XMLName.Local == "recipient" {
			rows = append(rows, child.row())
		}
	}
	return rows, nil
}

// logErrMsg returns the provider's human readable error field, if present.
func logErrMsg(response map[string]json.RawMessage) (string, bool) {
	raw, ok := response["err_msg"]
	if !ok {
		return "", false
	}
	var message string
	if json.Unmarshal(raw, &message) != nil {
		return "", false
	}
	message = strings.TrimSpace(message)
	return message, message != ""
}

// isLogEmptyReason reports whether the provider's err_msg only means that the
// query matched nothing. The documented wording is 未找到相应结果…; matching the
// distinguishing phrase keeps unrelated errors (quota, parameters) as errors.
func isLogEmptyReason(reason string) bool {
	return strings.Contains(reason, "未找到相应结果") || strings.Contains(strings.ToLower(reason), "not found")
}

func (c *Client) Logs(ctx context.Context, cred Credentials, q url.Values) ([]map[string]any, error) {
	b, err := c.get(ctx, cred, "maillog.php", q)
	if err != nil {
		return nil, err
	}
	if bytes.HasPrefix(b, []byte("<")) {
		_, err = parseXML(b)
		if err != nil {
			return nil, err
		}
		return nil, &Error{"unexpected_response", "日志接口未返回 JSON"}
	}
	var response map[string]json.RawMessage
	if err = json.Unmarshal(b, &response); err != nil {
		return nil, &Error{"invalid_json", "服务商返回的日志 JSON 无法解析"}
	}
	if raw, ok := response["status"]; ok {
		var status string
		_ = json.Unmarshal(raw, &status)
		if status != "" && status != "ok" && status != "success" {
			return nil, upstreamError(status)
		}
	}
	raw, ok := response["lists"]
	if !ok {
		// The provider signals "no rows for these filters" with HTTP 200 and
		// {"err_msg": "..."} instead of an empty lists array. That is a valid
		// empty result, not a protocol error.
		if reason, hasErr := logErrMsg(response); hasErr {
			if isLogEmptyReason(reason) {
				return []map[string]any{}, nil
			}
			return nil, &Error{"upstream_error", reason}
		}
		return nil, &Error{"unexpected_response", "日志响应缺少 lists 数组"}
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return []map[string]any{}, nil
	}
	rows := []map[string]any{}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err = dec.Decode(&rows); err != nil {
		return nil, &Error{"invalid_json", "日志列表不是有效的记录数组"}
	}
	if rows == nil {
		rows = []map[string]any{}
	}
	return rows, nil
}
