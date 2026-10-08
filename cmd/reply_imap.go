package main

import (
	"crypto/sha256"
	"crypto/tls"
	"database/sql"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"time"

	"github.com/emersion/go-imap"
	imapclient "github.com/emersion/go-imap/client"
)

func connectReplyIMAP(source replyAIMailboxSource, dialer *replyAIDialer) (*imapclient.Client, *imap.MailboxStatus, error) {
	port := source.Port
	if port == 0 {
		if source.TLSEnabled {
			port = 993
		} else {
			port = 143
		}
	}
	conn, err := dialer.Dial("tcp", net.JoinHostPort(source.Host, strconv.Itoa(port)))
	if err != nil {
		return nil, nil, err
	}
	if source.TLSEnabled {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: source.Host, MinVersion: tls.VersionTLS12})
		if err := tlsConn.Handshake(); err != nil {
			conn.Close()
			return nil, nil, err
		}
		conn = tlsConn
	}
	client, err := imapclient.New(conn)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	if err := client.Login(source.Username, source.Password); err != nil {
		client.Terminate()
		return nil, nil, err
	}
	folder := source.Folder
	if folder == "" {
		folder = "INBOX"
	}
	mailbox, err := client.Select(folder, true)
	if err != nil {
		client.Terminate()
		return nil, nil, err
	}
	return client, mailbox, nil
}

type replyIMAPConsumer struct {
	all             bool
	continueOnError bool
	load            func(uint32) (uint32, error)
	handle          func(uint32, []byte) error
	skip            func(uint32, string) error
	advance         func(uint32) error
}

func scanReplyIMAP(source replyAIMailboxSource, dialer *replyAIDialer, consumer replyIMAPConsumer) error {
	client, mailbox, err := connectReplyIMAP(source, dialer)
	if err != nil {
		return err
	}
	defer client.Terminate()
	last := uint32(0)
	if consumer.load != nil {
		last, err = consumer.load(mailbox.UidValidity)
		if err != nil {
			return err
		}
	}
	criteria := imap.NewSearchCriteria()
	criteria.Uid = new(imap.SeqSet)
	if last == ^uint32(0) {
		return nil
	}
	criteria.Uid.AddRange(last+1, 0)
	uids, err := client.UidSearch(criteria)
	if err != nil {
		return err
	}
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	processed := 0
	var skipped bool
	var messageErr error
	for _, uid := range uids {
		// IMAP '*' may resolve to the largest existing UID below our lower
		// bound. Always filter it locally to avoid re-reading the last item.
		if uid <= last {
			continue
		}
		if !consumer.all && processed >= replyAIMaxMessages {
			break
		}
		set := new(imap.SeqSet)
		set.AddNum(uid)
		metadata := make(chan *imap.Message, 1)
		if err := client.UidFetch(set, []imap.FetchItem{imap.FetchUid, imap.FetchRFC822Size}, metadata); err != nil {
			return err
		}
		msg := <-metadata
		if msg == nil {
			return fmt.Errorf("IMAP message UID %d disappeared before fetch; retry scan", uid)
		}
		if msg.Size > replyAIMaxMessageBytes {
			if consumer.skip == nil {
				err := fmt.Errorf("IMAP message UID %d exceeds the 5 MiB limit", uid)
				if !consumer.continueOnError {
					return err
				}
				// Forwarding revisits messages for retry and must still process
				// later messages when an oversized source cannot be forwarded.
				messageErr = err
				last = uid
				processed++
				continue
			}
			if err := consumer.skip(uid, "message_too_large"); err != nil {
				return err
			}
			skipped = true
		} else {
			section := &imap.BodySectionName{Peek: true, Partial: []int{0, replyAIMaxMessageBytes + 1}}
			messages := make(chan *imap.Message, 1)
			if err := client.UidFetch(set, []imap.FetchItem{imap.FetchUid, section.FetchItem()}, messages); err != nil {
				return err
			}
			msg := <-messages
			if msg == nil || msg.GetBody(section) == nil {
				return fmt.Errorf("IMAP message UID %d body missing", uid)
			}
			raw, err := io.ReadAll(io.LimitReader(msg.GetBody(section), replyAIMaxMessageBytes+1))
			if err != nil {
				return err
			}
			if len(raw) > replyAIMaxMessageBytes {
				return fmt.Errorf("IMAP message UID %d exceeds advertised size", uid)
			}
			if err := consumer.handle(uid, raw); err != nil {
				if !consumer.continueOnError {
					return err
				}
				messageErr = err
			}
		}
		if consumer.advance != nil {
			if err := consumer.advance(uid); err != nil {
				return err
			}
		}
		last = uid
		processed++
	}
	if skipped {
		return fmt.Errorf("oversized messages recorded as skipped; inspect reply AI events")
	}
	if messageErr != nil {
		return messageErr
	}
	return nil
}

// withReplyIMAPBudget closes the socket on timeout, including TLS greeting,
// authentication and folder selection. The dialer refuses new dials after close.
func withReplyIMAPBudget(budget replyAIScanBudget, run func(*replyAIDialer) error) error {
	dialer := &replyAIDialer{dialTimeout: budget.dial, readTimeout: budget.read}
	done := make(chan error, 1)
	go func() { done <- run(dialer) }()
	if budget.scan <= 0 {
		return <-done
	}
	timer := time.NewTimer(budget.scan)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
	}
	dialer.Close()
	select {
	case <-done:
	case <-time.After(replyAIScanCloseGrace):
	}
	return fmt.Errorf("%w after %s", errReplyAIScanTimeout, budget.scan)
}

func (a *App) replyIMAPCursor(source replyAIMailboxSource, consumer string, handle func(uint32, []byte) error) replyIMAPConsumer {
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%t\x00%s\x00%s\x00%s", source.Host, source.Port, source.TLSEnabled, source.Folder, source.Username, source.Password))))
	var validity uint32
	return replyIMAPConsumer{
		load: func(v uint32) (uint32, error) {
			validity = v
			var uid uint32
			err := a.db.Get(&uid, `SELECT last_uid FROM reply_mailbox_scan_cursors WHERE mailbox_id=$1 AND consumer=$2 AND connection_hash=$3 AND uid_validity=$4`, source.ID, consumer, hash, v)
			if err == sql.ErrNoRows {
				return 0, nil
			}
			return uid, err
		},
		handle: handle,
		advance: func(uid uint32) error {
			_, err := a.db.Exec(`INSERT INTO reply_mailbox_scan_cursors(mailbox_id,consumer,connection_hash,uid_validity,last_uid) VALUES($1,$2,$3,$4,$5)
			ON CONFLICT(mailbox_id,consumer) DO UPDATE SET connection_hash=EXCLUDED.connection_hash,uid_validity=EXCLUDED.uid_validity,
			last_uid=CASE WHEN reply_mailbox_scan_cursors.connection_hash=EXCLUDED.connection_hash AND reply_mailbox_scan_cursors.uid_validity=EXCLUDED.uid_validity THEN GREATEST(reply_mailbox_scan_cursors.last_uid,EXCLUDED.last_uid) ELSE EXCLUDED.last_uid END`, source.ID, consumer, hash, validity, uid)
			return err
		},
		skip: func(uid uint32, reason string) error {
			if consumer != "ai" {
				return fmt.Errorf("forwarding message UID %d exceeds size limit", uid)
			}
			_, err := a.db.Exec(`INSERT INTO reply_ai_events(reply_mailbox_id,message_key,status,action,reason_code)
			VALUES($1,$2,'ignored','ignored',$3) ON CONFLICT(reply_mailbox_id,message_key) DO NOTHING`, source.ID, fmt.Sprintf("imap:%s:%d:%d", hash, validity, uid), reason)
			return err
		},
	}
}
