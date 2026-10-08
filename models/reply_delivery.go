package models

import (
	"fmt"
	"regexp"
	"strconv"
)

var replyMessageIDPattern = regexp.MustCompile(`<lm\.([0-9a-fA-F-]{36})\.([cp])\.([0-9]+)@reply\.listmonk>`)

type ReplyReference struct {
	CampaignUUID  string
	CustomerID    int
	PoolContactID int64
}

func CampaignReplyMessageID(campaignUUID string, customerID int, poolContactID int64) string {
	if campaignUUID == "" {
		return ""
	}
	kind, id := "c", int64(customerID)
	if poolContactID > 0 {
		kind, id = "p", poolContactID
	}
	if id <= 0 {
		return ""
	}
	return fmt.Sprintf("<lm.%s.%s.%d@reply.listmonk>", campaignUUID, kind, id)
}

func ParseReplyReferences(header string) []ReplyReference {
	var out []ReplyReference
	for _, match := range replyMessageIDPattern.FindAllStringSubmatch(header, -1) {
		id, err := strconv.ParseInt(match[3], 10, 64)
		if err != nil || id <= 0 {
			continue
		}
		ref := ReplyReference{CampaignUUID: match[1]}
		if match[2] == "c" {
			ref.CustomerID = int(id)
		} else {
			ref.PoolContactID = id
		}
		out = append(out, ref)
	}
	return out
}
