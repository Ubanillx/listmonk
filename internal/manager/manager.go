package manager

import (
	"errors"
	"fmt"
	"html/template"
	"log"
	"mime"
	"net/textproto"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"maps"

	"github.com/Masterminds/sprig/v3"
	"github.com/knadh/listmonk/internal/i18n"
	"github.com/knadh/listmonk/internal/messenger/email"
	"github.com/knadh/listmonk/internal/notifs"
	"github.com/knadh/listmonk/models"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

const (
	// BaseTPL is the name of the base template.
	BaseTPL = "base"

	// ContentTpl is the name of the compiled message.
	ContentTpl = "content"

	dummyUUID = "00000000-0000-0000-0000-000000000000"
)

var ErrCampaignDeferred = errors.New("campaign deferred")

// ErrManagerClosed is returned when a caller attempts to enqueue work after
// the campaign manager has begun shutting down. Keeping one sentinel lets
// HTTP/transactional callers distinguish an intentional reload from a
// transient queue timeout without having to match an error string.
var ErrManagerClosed = errors.New("campaign manager is closed")

// ErrPersonalSMTPUnavailable is returned when a user-originated e-mail has
// no enabled account SMTP pool. It is deliberately distinct from generic
// connection errors so a running campaign can be paused immediately instead
// of repeatedly retrying (or ever falling back to the platform SMTP).
var ErrPersonalSMTPUnavailable = errors.New("personal SMTP unavailable")

// Store represents a data backend, such as a database,
// that provides subscriber and campaign records.
type Store interface {
	NextCampaigns(currentIDs []int64) ([]*models.Campaign, error)
	NextSubscribers(campID, limit int) ([]models.CampaignSubscriber, error)
	GetCampaign(campID int) (*models.Campaign, error)
	GetAttachment(mediaID int) (models.Attachment, error)
	UpdateCampaignStatus(campID int, status string) error
	UpdateCampaignCounts(campID int, toSend int, sent int, lastSubID int) error
	MarkCampaignMessageSent(campID int, subID int) error
	MarkCampaignRecipientStatus(campID int, subID int, status string) error
	ResetCampaignQueuedRecipients(campID int, toStatus string) error
	UpdateCampaignRecipientStatuses(campID int, toStatus string, fromStatuses []string) error
	DeferCampaign(campID int, nextResumeAt time.Time) error
	CreateLink(campUUID, url string) (string, error)
	BlocklistSubscriber(id int64) error
	DeleteSubscriber(id int64) error
}

// CampaignAttachmentStore is an optional extension implemented by the
// database-backed store.  Campaign media IDs are loaded by the scheduler from
// a shared queue, so an ID-only lookup is not sufficient: a media row can be
// transferred or removed after the campaign was selected.  Stores that can
// validate the campaign's association and workspace boundary should implement
// this method; Manager falls back to GetAttachment only for legacy/test stores
// that do not provide the extension.
type CampaignAttachmentStore interface {
	GetCampaignAttachments(campaign *models.Campaign, mediaIDs []int64) ([]models.Attachment, error)
}

// PersonalSMTPUnavailableStore is an optional store extension used when a
// campaign is claimed by the scheduler but its owner's SMTP pool cannot be
// resolved. Implementations should atomically persist the strict fallback
// state (draft for scheduled/deferred claims, paused for running claims),
// clear scheduling timestamps, and return queued recipients to pending. The
// manager keeps a legacy UpdateCampaignStatus fallback for stores that do not
// implement this extension.
type PersonalSMTPUnavailableStore interface {
	MarkCampaignSMTPUnavailable(campaignID int, previousStatus string) error
}

// CampaignStartFailureStore is an optional store extension used when a
// campaign cannot be initialized after the scheduler has claimed it.  The
// implementation must atomically move a scheduled/deferred claim back to
// draft (or pause an already-running campaign), clear scheduling timestamps,
// and return queued recipients to pending.  Keeping this optional preserves
// compatibility with the lightweight stores used by integrations and tests.
type CampaignStartFailureStore interface {
	MarkCampaignStartFailure(campaignID int, previousStatus string) error
}

// campaignStartFailureStatus returns the safe state for a campaign that could
// not be initialized.  A scheduled/deferred claim has not delivered anything
// in this process and can be edited as a draft; a campaign that was already
// running must be paused so an operator can repair it without losing its
// historical send state.
func campaignStartFailureStatus(previousStatus string) string {
	if previousStatus == models.CampaignStatusRunning {
		return models.CampaignStatusPaused
	}
	return models.CampaignStatusDraft
}

// markCampaignStartFailure persists a safe state after initialization fails.
// Production stores use the atomic extension; the fallback deliberately
// resets queued recipients before changing the status so a retried campaign
// cannot strand recipients in the queued state.
func (m *Manager) markCampaignStartFailure(c *models.Campaign) {
	if c == nil || m.store == nil {
		return
	}
	previous := c.SchedulerStatus
	if previous == "" {
		previous = c.Status
	}
	if s, ok := m.store.(CampaignStartFailureStore); ok {
		if err := s.MarkCampaignStartFailure(c.ID, previous); err != nil {
			m.log.Printf("error marking campaign (%s) start failure: %v", c.Name, err)
		}
		return
	}
	if err := m.store.ResetCampaignQueuedRecipients(c.ID, models.CampaignRecipientStatusPending); err != nil {
		m.log.Printf("error resetting queued recipients (%s): %v", c.Name, err)
	}
	status := campaignStartFailureStatus(previous)
	if err := m.store.UpdateCampaignStatus(c.ID, status); err != nil {
		m.log.Printf("error updating campaign (%s) status after start failure: %v", c.Name, err)
	}
}

// markCampaignSMTPUnavailable persists the strict no-fallback state after a
// scheduler claim cannot resolve the campaign owner's SMTP pool. The optional
// store method performs the transition transactionally; the fallback keeps
// compatibility with lightweight test stores.
func (m *Manager) markCampaignSMTPUnavailable(c *models.Campaign) {
	if c == nil || m.store == nil {
		return
	}
	previous := c.SchedulerStatus
	if previous == "" {
		previous = c.Status
	}
	if s, ok := m.store.(PersonalSMTPUnavailableStore); ok {
		if err := s.MarkCampaignSMTPUnavailable(c.ID, previous); err != nil {
			m.log.Printf("error marking campaign (%s) SMTP unavailable: %v", c.Name, err)
		}
		return
	}
	// Reuse the generic lifecycle transition for stores that do not expose the
	// historical SMTP-specific method.  This also guarantees queued recipient
	// cleanup in the fallback path.
	m.markCampaignStartFailure(c)
}

// Messenger is an interface for a generic messaging backend,
// for instance, e-mail, SMS etc.
type Messenger interface {
	Name() string
	Push(models.Message) error
	Flush() error
	Close() error
}

// CampStats contains campaign stats like per minute send rate.
type CampStats struct {
	SendRate int
}

// Manager handles the scheduling, processing, and queuing of campaigns
// and message pushes.
type Manager struct {
	cfg                Config
	store              Store
	i18n               *i18n.I18n
	messengers         map[string]Messenger
	personalSMTP       func(int) (*email.Emailer, error)
	personalMessengers map[int]*email.Emailer
	personalSMTPMut    sync.Mutex
	// personalSMTPSendMut coordinates account-owned sends with cache
	// invalidation. Readers (sends) may run concurrently, while a
	// configuration change takes the writer lock before closing the old pool;
	// this prevents a message from using a pool after the update has completed.
	personalSMTPSendMut sync.RWMutex
	fnNotify            func(subject string, data any) error
	log                 *log.Logger

	// Campaigns that are currently running.
	pipes    map[int]*pipe
	pipesMut sync.RWMutex

	tpls    map[int]*models.Template
	tplsMut sync.RWMutex

	// Links generated using Track() are cached per campaign and URL so an
	// association is persisted for every campaign that emits a shared URL. This
	// has to be locked as it may be used externally when previewing campaigns.
	links    map[string]string
	linksMut sync.RWMutex

	nextPipes chan *pipe
	campMsgQ  chan CampaignMessage
	msgQ      chan models.Message

	// done is closed once during shutdown. Queues intentionally remain open:
	// the scanner and HTTP handlers can race with a reload, and closing a queue
	// while a producer is sending would panic. Producers and workers select on
	// done instead, giving shutdown one race-safe cancellation path.
	done      chan struct{}
	closeOnce sync.Once
	closed    atomic.Bool

	// Sliding window keeps track of the total number of messages sent in a period
	// and on reaching the specified limit, waits until the window is over before
	// sending further messages.
	slidingCount int
	slidingStart time.Time

	tplFuncs template.FuncMap
}

// CampaignMessage represents an instance of campaign message to be pushed out,
// specific to a subscriber, via the campaign's messenger.
type CampaignMessage struct {
	Campaign   *models.Campaign
	Subscriber models.Subscriber

	from     string
	to       string
	subject  string
	body     []byte
	altBody  []byte
	unsubURL string

	pipe *pipe
}

// Config has parameters for configuring the manager.
type Config struct {
	// Number of subscribers to pull from the DB in a single iteration.
	BatchSize             int
	Concurrency           int
	MessageRate           int
	MaxSendErrors         int
	SlidingWindow         bool
	SlidingWindowDuration time.Duration
	SlidingWindowRate     int
	RequeueOnError        bool
	FromEmail             string
	IndividualTracking    bool
	DisableTracking       bool
	LinkTrackURL          string
	UnsubURL              string
	OptinURL              string
	MessageURL            string
	ViewTrackURL          string
	ArchiveURL            string
	RootURL               string
	UnsubHeader           bool

	// Interval to scan the DB for active campaign checkpoints.
	ScanInterval time.Duration

	// ScanCampaigns indicates whether this instance of manager will scan the DB
	// for active campaigns and process them.
	// This can be used to run multiple instances of listmonk
	// (exposed to the internet, private etc.) where only one does campaign
	// processing while the others handle other kinds of traffic.
	ScanCampaigns bool

	// PersonalSMTP resolves the enabled SMTP pool for an account. A nil
	// resolver means account-owned campaign/transactional sends are disabled.
	PersonalSMTP func(int) (*email.Emailer, error)
}

var pushTimeout = time.Second * 3

// New returns a new instance of Mailer.
func New(cfg Config, store Store, i *i18n.I18n, l *log.Logger) *Manager {
	if cfg.BatchSize < 1 {
		cfg.BatchSize = 1000
	}
	if cfg.Concurrency < 1 {
		cfg.Concurrency = 1
	}
	if cfg.MessageRate < 1 {
		cfg.MessageRate = 1
	}

	m := &Manager{
		cfg:   cfg,
		store: store,
		i18n:  i,
		fnNotify: func(subject string, data any) error {
			return notifs.NotifySystem(subject, notifs.TplCampaignStatus, data, nil)
		},
		log:                l,
		messengers:         make(map[string]Messenger),
		personalSMTP:       cfg.PersonalSMTP,
		personalMessengers: make(map[int]*email.Emailer),
		pipes:              make(map[int]*pipe),
		tpls:               make(map[int]*models.Template),
		links:              make(map[string]string),
		nextPipes:          make(chan *pipe, 1000),
		campMsgQ:           make(chan CampaignMessage, cfg.Concurrency*cfg.MessageRate*2),
		msgQ:               make(chan models.Message, cfg.Concurrency*cfg.MessageRate*2),
		done:               make(chan struct{}),
		slidingStart:       time.Now(),
	}
	m.tplFuncs = m.makeGnericFuncMap()

	return m
}

// AddMessenger adds a Messenger messaging backend to the manager.
func (m *Manager) AddMessenger(msg Messenger) error {
	id := msg.Name()
	if _, ok := m.messengers[id]; ok {
		return fmt.Errorf("messenger '%s' is already loaded", id)
	}
	m.messengers[id] = msg

	return nil
}

// PushMessage pushes an arbitrary non-campaign Message to be sent out by the workers.
// It times out if the queue is busy.
func (m *Manager) PushMessage(msg models.Message) error {
	if m.closed.Load() {
		return ErrManagerClosed
	}
	select {
	case <-m.done:
		return ErrManagerClosed
	default:
	}

	t := time.NewTicker(pushTimeout)
	defer t.Stop()

	select {
	case m.msgQ <- msg:
	case <-m.done:
		return ErrManagerClosed
	case <-t.C:
		m.log.Printf("message push timed out: '%s'", msg.Subject)
		return errors.New("message push timed out")
	}

	return nil
}

// PushCampaignMessage pushes a campaign messages into a queue to be sent out by the workers.
// It times out if the queue is busy.
func (m *Manager) PushCampaignMessage(msg CampaignMessage) error {
	if m.closed.Load() {
		return ErrManagerClosed
	}
	select {
	case <-m.done:
		return ErrManagerClosed
	default:
	}

	t := time.NewTicker(pushTimeout)
	defer t.Stop()

	// Load any media/attachments.
	if err := m.attachMedia(msg.Campaign); err != nil {
		return err
	}

	select {
	case m.campMsgQ <- msg:
	case <-m.done:
		return ErrManagerClosed
	case <-t.C:
		m.log.Printf("message push timed out: '%s'", msg.Subject())
		return errors.New("message push timed out")
	}

	return nil
}

// HasMessenger checks if a given messenger is registered.
func (m *Manager) HasMessenger(id string) bool {
	_, ok := m.messengers[id]

	return ok
}

func (m *Manager) CanSendMessage(msg models.Message) error {
	if m.closed.Load() {
		return ErrManagerClosed
	}
	if m.isPersonalSMTPMessage(msg) {
		m.personalSMTPSendMut.RLock()
		defer m.personalSMTPSendMut.RUnlock()
		if m.closed.Load() {
			return ErrManagerClosed
		}
	}

	msgr, err := m.resolveMessenger(msg)
	if err != nil {
		return m.normalizePersonalSMTPError(msg, err)
	}

	if p, ok := msgr.(interface{ CanSend(models.Message) error }); ok {
		return m.normalizePersonalSMTPError(msg, p.CanSend(msg))
	}

	return nil
}

func (m *Manager) isPersonalSMTPMessage(msg models.Message) bool {
	return email.IsMessengerName(msg.Messenger) &&
		(msg.OwnerUserID > 0 || msg.Campaign != nil)
}

// resolveMessenger isolates account-owned SMTP traffic from the platform
// messenger. OwnerUserID == 0 is reserved for system notifications.
func (m *Manager) resolveMessenger(msg models.Message) (Messenger, error) {
	if m.closed.Load() {
		return nil, ErrManagerClosed
	}
	if m.isPersonalSMTPMessage(msg) {
		if msg.OwnerUserID < 1 {
			return nil, fmt.Errorf("%w: campaign has no account owner", ErrPersonalSMTPUnavailable)
		}
		if m.personalSMTP == nil {
			return nil, fmt.Errorf("%w: no personal SMTP configured for user %d", ErrPersonalSMTPUnavailable, msg.OwnerUserID)
		}
		m.personalSMTPMut.Lock()
		defer m.personalSMTPMut.Unlock()
		if m.closed.Load() {
			return nil, ErrManagerClosed
		}
		if msgr, ok := m.personalMessengers[msg.OwnerUserID]; ok {
			return msgr, nil
		}
		msgr, err := m.personalSMTP(msg.OwnerUserID)
		if err != nil {
			// A resolver failure means the account-owned pool cannot be
			// guaranteed.  Normalize it to the strict sentinel so scheduled
			// campaigns are drafted and running campaigns pause instead of
			// retrying (or ever falling back to the platform SMTP).
			if errors.Is(err, ErrPersonalSMTPUnavailable) {
				return nil, err
			}
			return nil, fmt.Errorf("%w: %v", ErrPersonalSMTPUnavailable, err)
		}
		if msgr == nil {
			return nil, fmt.Errorf("%w: no personal SMTP configured for user %d", ErrPersonalSMTPUnavailable, msg.OwnerUserID)
		}
		m.personalMessengers[msg.OwnerUserID] = msgr
		return msgr, nil
	}
	msgr, ok := m.messengers[msg.Messenger]
	if !ok {
		return nil, fmt.Errorf("unknown messenger %s", msg.Messenger)
	}
	return msgr, nil
}

// pushMessage resolves and sends one message. Account-owned SMTP sends hold a
// read lock for the complete operation so InvalidatePersonalSMTP can wait for
// an in-flight delivery, close the old pool, and prevent subsequent sends from
// borrowing it.
func (m *Manager) pushMessage(msg models.Message) error {
	if m.closed.Load() {
		return ErrManagerClosed
	}
	if m.isPersonalSMTPMessage(msg) {
		m.personalSMTPSendMut.RLock()
		defer m.personalSMTPSendMut.RUnlock()
		if m.closed.Load() {
			return ErrManagerClosed
		}
	}

	msgr, err := m.resolveMessenger(msg)
	if err != nil {
		return m.normalizePersonalSMTPError(msg, err)
	}
	return m.normalizePersonalSMTPError(msg, msgr.Push(msg))
}

// normalizePersonalSMTPError maps errors from the account-owned SMTP pool to
// the manager-level sentinel used by campaign workers. In particular, a pool
// can be closed while its credentials are being replaced; that condition must
// pause the campaign immediately instead of being counted as a transient send
// error (and potentially retried against stale credentials).
func (m *Manager) normalizePersonalSMTPError(msg models.Message, err error) error {
	if err == nil || !m.isPersonalSMTPMessage(msg) {
		return err
	}
	if errors.Is(err, ErrPersonalSMTPUnavailable) {
		return err
	}
	if errors.Is(err, email.ErrSMTPUnavailable) {
		return fmt.Errorf("%w: %v", ErrPersonalSMTPUnavailable, err)
	}
	return err
}

// InvalidatePersonalSMTP closes a cached account pool after configuration
// changes. Callers enforce the strict running-campaign guard before calling it.
func (m *Manager) InvalidatePersonalSMTP(userID int) {
	if userID < 1 {
		return
	}
	// Wait for any in-flight account-owned deliveries before closing the pool.
	// Once this writer lock is acquired, new sends are blocked until the cache
	// has been replaced (or removed).
	m.personalSMTPSendMut.Lock()
	defer m.personalSMTPSendMut.Unlock()

	m.personalSMTPMut.Lock()
	defer m.personalSMTPMut.Unlock()
	if msgr, ok := m.personalMessengers[userID]; ok {
		_ = msgr.Close()
		delete(m.personalMessengers, userID)
	}
}

// WithPersonalSMTPUpdate serializes an account SMTP configuration change with
// every account-owned delivery. The callback runs while the manager's writer
// lock is held, so no message can resolve or borrow a pool backed by the old
// database rows between the configuration transaction and cache invalidation.
//
// A non-positive user ID invalidates every cached account pool. This form is
// used by platform user deletion, where the SMTP rows for several accounts can
// be removed atomically by one database operation.
func (m *Manager) WithPersonalSMTPUpdate(userID int, fn func() error) error {
	if fn == nil {
		return nil
	}

	m.personalSMTPSendMut.Lock()
	defer m.personalSMTPSendMut.Unlock()
	m.personalSMTPMut.Lock()
	defer m.personalSMTPMut.Unlock()

	if userID > 0 {
		if msgr, ok := m.personalMessengers[userID]; ok {
			_ = msgr.Close()
			delete(m.personalMessengers, userID)
		}
	} else {
		for id, msgr := range m.personalMessengers {
			_ = msgr.Close()
			delete(m.personalMessengers, id)
		}
	}

	return fn()
}

// HasRunningCampaigns checks if there are any active campaigns.
func (m *Manager) HasRunningCampaigns() bool {
	m.pipesMut.Lock()
	defer m.pipesMut.Unlock()

	return len(m.pipes) > 0
}

// GetCampaignStats returns campaign statistics.
func (m *Manager) GetCampaignStats(id int) CampStats {
	n := 0

	m.pipesMut.Lock()
	if c, ok := m.pipes[id]; ok {
		n = int(c.rate.Rate())
	}
	m.pipesMut.Unlock()

	return CampStats{SendRate: n}
}

// Run is a blocking function (that should be invoked as a goroutine)
// that scans the data source at regular intervals for pending campaigns,
// and queues them for processing. The process queue fetches batches of
// subscribers and pushes messages to them for each queued campaign
// until all subscribers are exhausted, at which point, a campaign is marked
// as "finished".
func (m *Manager) Run() {
	if m.cfg.ScanCampaigns {
		// Periodically scan campaigns and push running campaigns to nextPipes
		// to fetch subscribers from the campaign.
		go m.scanCampaigns(m.cfg.ScanInterval)
	}

	// Spawn N message workers.
	for i := 0; i < m.cfg.Concurrency; i++ {
		go m.worker()
	}

	// Indefinitely wait on the pipe queue to fetch the next set of subscribers
	// for any active campaigns.
	for {
		select {
		case <-m.done:
			return
		case p := <-m.nextPipes:
			if p == nil {
				continue
			}
			// A buffered pipe may become ready at the same time as the shutdown
			// signal. Do not fetch another subscriber batch after Close; release
			// the pipe's scheduler counter so its cleanup goroutine cannot hang.
			if m.closed.Load() {
				p.Stop(stopReasonPause, false)
				p.wg.Done()
				continue
			}
			has, err := p.NextSubscribers()
			if err != nil {
				m.log.Printf("error processing campaign batch (%s): %v", p.camp.Name, err)

				// If the batch fails, stop the pipe and release it so that it doesn't hang forever.
				// The cleanup() records the state in DB and scanCampaigns() picks it up at a later point.
				p.Stop(stopReasonPause, false)
				p.wg.Done()
				continue
			}

			if has {
				// There are more subscribers to fetch. Queue again.
				select {
				case m.nextPipes <- p:
				case <-m.done:
					p.Stop(stopReasonPause, false)
					p.wg.Done()
				default:
					// If the queue is full for any reason, stop the pipe and release it.
					// The cleanup() records the state in DB and scanCampaigns() picks it up
					// at a later point.
					p.Stop(stopReasonPause, false)
					p.wg.Done()
				}
			} else {
				// The pipe is created with a +1 on the waitgroup pseudo counter
				// so that it immediately waits. Subsequently, every message created
				// is incremented in the counter in pipe.newMessage(), and when it's'
				// processed (or ignored when a campaign is paused or cancelled),
				// the count is's reduced in worker().
				//
				// This marks down the original non-message +1, causing the waitgroup
				// to be released and the pipe to end, triggering the pg.Wait()
				// in newPipe() that calls pipe.cleanup().
				p.wg.Done()
			}
		}
	}
}

// CacheTpl caches a template for ad-hoc use. This is currently only used by tx templates.
func (m *Manager) CacheTpl(id int, tpl *models.Template) {
	m.tplsMut.Lock()
	m.tpls[id] = tpl
	m.tplsMut.Unlock()
}

// DeleteTpl deletes a cached template.
func (m *Manager) DeleteTpl(id int) {
	m.tplsMut.Lock()
	delete(m.tpls, id)
	m.tplsMut.Unlock()
}

// GetTpl returns a cached template.
func (m *Manager) GetTpl(id int) (*models.Template, error) {
	m.tplsMut.RLock()
	tpl, ok := m.tpls[id]
	m.tplsMut.RUnlock()

	if !ok {
		return nil, fmt.Errorf("template %d not found", id)
	}

	return tpl, nil
}

// TemplateFuncs returns the template functions to be applied into
// compiled campaign templates.
func (m *Manager) TemplateFuncs(c *models.Campaign) template.FuncMap {
	f := template.FuncMap{
		"TrackLink": func(url string, msg *CampaignMessage) string {
			if m.cfg.DisableTracking {
				return url
			}

			subUUID := msg.Subscriber.UUID
			if !m.cfg.IndividualTracking {
				subUUID = dummyUUID
			}

			return m.trackLink(url, msg.Campaign.UUID, subUUID)
		},
		"TrackView": func(msg *CampaignMessage) template.HTML {
			if m.cfg.DisableTracking {
				return template.HTML("")
			}

			subUUID := msg.Subscriber.UUID
			if !m.cfg.IndividualTracking {
				subUUID = dummyUUID
			}

			return template.HTML(fmt.Sprintf(`<img src="%s" alt="" />`,
				fmt.Sprintf(m.cfg.ViewTrackURL, msg.Campaign.UUID, subUUID)))
		},
		"UnsubscribeURL": func(msg *CampaignMessage) string {
			return msg.unsubURL
		},
		"ManageURL": func(msg *CampaignMessage) string {
			return msg.unsubURL + "?manage=true"
		},
		"OptinURL": func(msg *CampaignMessage) string {
			// Add list IDs.
			// TODO: Show private lists list on optin e-mail
			return fmt.Sprintf(m.cfg.OptinURL, msg.Subscriber.UUID, "")
		},
		"MessageURL": func(msg *CampaignMessage) string {
			return fmt.Sprintf(m.cfg.MessageURL, c.UUID, msg.Subscriber.UUID)
		},
		"ArchiveURL": func() string {
			return m.cfg.ArchiveURL
		},
		"RootURL": func() string {
			return m.cfg.RootURL
		},
	}

	maps.Copy(f, m.tplFuncs)

	return f
}

func (m *Manager) GenericTemplateFuncs() template.FuncMap {
	return m.tplFuncs
}

// StopCampaign marks a running campaign as stopped so that all its queued messages are ignored.
func (m *Manager) StopCampaign(id int, status string) {
	m.pipesMut.RLock()
	if p, ok := m.pipes[id]; ok {
		reason := stopReasonPause
		if status == models.CampaignStatusCancelled {
			reason = stopReasonCancelled
		}
		p.Stop(reason, false)
	}
	m.pipesMut.RUnlock()
}

// Close closes and exits the campaign manager.
func (m *Manager) Close() {
	m.closeOnce.Do(func() {
		m.closed.Store(true)
		close(m.done)
		m.personalSMTPSendMut.Lock()
		defer m.personalSMTPSendMut.Unlock()
		m.personalSMTPMut.Lock()
		defer m.personalSMTPMut.Unlock()
		for userID, msgr := range m.personalMessengers {
			_ = msgr.Close()
			delete(m.personalMessengers, userID)
		}
	})
}

// scanCampaigns is a blocking function that periodically scans the data source
// for campaigns to process and dispatches them to the manager. It feeds campaigns
// into nextPipes.
func (m *Manager) scanCampaigns(tick time.Duration) {
	t := time.NewTicker(tick)
	defer t.Stop()

	// Periodically scan the data source for campaigns to process.
	for {
		select {
		case <-m.done:
			return
		case <-t.C:
		}

		ids := m.getCurrentCampaigns()
		campaigns, err := m.store.NextCampaigns(ids)
		if err != nil {
			m.log.Printf("error fetching campaigns: %v", err)
			continue
		}

		for _, c := range campaigns {
			select {
			case <-m.done:
				return
			default:
			}

			// Create a new pipe that'll handle this campaign's states.
			p, err := m.newPipe(c)
			if err != nil {
				m.log.Printf("error processing campaign (%s): %v", c.Name, err)
				continue
			}
			m.log.Printf("start processing campaign (%s)", c.Name)

			// If subscriber processing is busy, move on. Blocking and waiting
			// can end up in a race condition where the waiting campaign's
			// state in the data source has changed.
			select {
			case m.nextPipes <- p:
			case <-m.done:
				p.Stop(stopReasonPause, false)
				p.wg.Done()
			default:
				// If the queue is full for any reason, stop the pipe and release it.
				// The cleanup() records the state in DB and scanCampaigns() picks it up
				// at a later point.
				p.Stop(stopReasonPause, false)
				p.wg.Done()
			}
		}
	}
}

// worker is a blocking function that perpetually listents to events (message) on different
// queues and processes them.
func (m *Manager) worker() {
	// Counter to keep track of the message / sec rate limit.
	numMsg := 0
	for {
		// Prefer shutdown over already-buffered work. The process is about to
		// reload, so accepting another message could race pool invalidation.
		select {
		case <-m.done:
			return
		default:
		}

		select {
		case <-m.done:
			return
		// Campaign message.
		case msg := <-m.campMsgQ:
			// The first shutdown check above and this receive are separate
			// operations; a buffered message can still win the select alongside
			// done. Drop it explicitly and account for its wait-group reference.
			if m.closed.Load() {
				if msg.pipe != nil {
					msg.pipe.Stop(stopReasonPause, false)
					msg.pipe.wg.Done()
				}
				continue
			}

			// If the campaign has ended or stopped, ignore the message.
			if msg.pipe != nil && msg.pipe.stopped.Load() {
				// Reduce the message counter on the pipe.
				msg.pipe.wg.Done()
				continue
			}

			// Pause on hitting the message rate.
			if numMsg >= m.cfg.MessageRate {
				time.Sleep(time.Second)
				numMsg = 0
			}
			numMsg++

			// Outgoing message.
			body := msg.body
			attachments := msg.Campaign.Attachments
			if msg.Campaign.ContentType != models.CampaignContentTypePlain && email.IsMessengerName(msg.Campaign.Messenger) {
				body, attachments = InlineMediaImages(body, attachments)
			}

			out := models.Message{
				From:         msg.from,
				To:           []string{msg.to},
				Subject:      msg.subject,
				ContentType:  msg.Campaign.ContentType,
				Body:         body,
				AltBody:      msg.altBody,
				Subscriber:   msg.Subscriber,
				Messenger:    msg.Campaign.Messenger,
				UseSMTPFrom:  email.IsMessengerName(msg.Campaign.Messenger),
				UseSMTPQuota: email.IsMessengerName(msg.Campaign.Messenger),
				Campaign:     msg.Campaign,
				Attachments:  attachments,
			}

			h := textproto.MIMEHeader{}
			h.Set(models.EmailHeaderCampaignUUID, msg.Campaign.UUID)
			h.Set(models.EmailHeaderSubscriberUUID, msg.Subscriber.UUID)

			// Attach List-Unsubscribe headers?
			if m.cfg.UnsubHeader {
				h.Set("List-Unsubscribe-Post", "List-Unsubscribe=One-Click")
				h.Set("List-Unsubscribe", `<`+msg.unsubURL+`>`)
			}

			// Attach any custom headers.
			if len(msg.Campaign.Headers) > 0 {
				for _, set := range msg.Campaign.Headers {
					for hdr, val := range set {
						h.Add(hdr, val)
					}
				}
			}
			// Reply-To is controlled by the selected, verified customer-reply
			// mailbox. Apply it after custom headers so a campaign cannot spoof or
			// overwrite the account-owned destination.
			if msg.Campaign.ReplyMailboxEmail != "" {
				h.Set("Reply-To", msg.Campaign.ReplyMailboxEmail)
			}

			// Set the headers.
			out.Headers = h
			// Close may have raced with rendering the message. Do not enqueue a
			// delivery after shutdown; the pipe cleanup will return queued rows to
			// pending.
			if m.closed.Load() {
				if msg.pipe != nil {
					msg.pipe.Stop(stopReasonPause, false)
					msg.pipe.wg.Done()
				}
				continue
			}

			// Resolve the account-owned SMTP pool from the campaign owner. System
			// messages have OwnerUserID == 0 and use the platform messenger. For a
			// regular campaign, keep the old immutable messenger path; personal
			// SMTP campaigns deliberately resolve through pushMessage so the
			// invalidation read/write lock covers the complete delivery.
			out.OwnerUserID = msg.Campaign.OwnerUserID.Int
			var err error
			if msg.pipe != nil && msg.pipe.messenger != nil {
				err = msg.pipe.messenger.Push(out)
			} else {
				err = m.pushMessage(out)
			}
			if err != nil {
				m.log.Printf("error sending message in campaign %s: subscriber %d: %v", msg.Campaign.Name, msg.Subscriber.ID, err)
			}

			// Increment the send rate or the error counter if there was an error.
			if msg.pipe != nil {
				if errors.Is(err, ErrManagerClosed) {
					// Shutdown is an intentional cancellation, not a delivery
					// failure. Stop the pipe and leave its recipients retryable.
					msg.pipe.Stop(stopReasonPause, false)
				} else if errors.Is(err, ErrPersonalSMTPUnavailable) {
					// A user's SMTP pool was disabled or removed while the
					// campaign was running. Stop immediately and leave unsent
					// recipients pending for a later operator restart.
					msg.pipe.Stop(stopReasonPersonalSMTP, false)
				} else if errors.Is(err, email.ErrSMTPQuotaExceeded) {
					m.log.Printf("smtp daily quota exhausted for campaign %s (id=%d). deferring until daily_resume_time",
						msg.Campaign.Name,
						msg.Campaign.ID,
					)
					msg.pipe.Defer()
				} else if err != nil {
					if uErr := m.store.MarkCampaignRecipientStatus(msg.Campaign.ID, msg.Subscriber.ID, models.CampaignRecipientStatusPending); uErr != nil {
						m.log.Printf("error resetting campaign recipient (%s:%d): %v", msg.Campaign.Name, msg.Subscriber.ID, uErr)
					}
					// Call the error callback, which keeps track of the error count
					// and stops the campaign if the error count exceeds the threshold.
					msg.pipe.OnError()
				} else {
					if uErr := m.store.MarkCampaignMessageSent(msg.Campaign.ID, msg.Subscriber.ID); uErr != nil {
						m.log.Printf("error updating campaign recipient (%s:%d): %v", msg.Campaign.Name, msg.Subscriber.ID, uErr)
					}
					msg.pipe.rate.Incr(1)
					msg.pipe.sent.Add(1)
				}

				// Mark the message as done only after its recipient state and any
				// stop/defer signal have been persisted. Doing this first lets the
				// cleanup goroutine race ahead and incorrectly mark a quota or SMTP
				// failure as a naturally finished campaign.
				msg.pipe.wg.Done()
			}

		// Arbitrary message.
		case msg := <-m.msgQ:
			if m.closed.Load() {
				continue
			}

			// Arbitrary messages with an OwnerUserID use that account's SMTP pool;
			// system notifications leave it zero and use the platform messenger.
			if err := m.pushMessage(msg); err != nil {
				m.log.Printf("error sending message '%s': %v", msg.Subject, err)
			}
		}
	}
}

// getCurrentCampaigns returns the IDs of campaigns currently being processed
// so that scanCampaigns doesn't duplicate work.
func (m *Manager) getCurrentCampaigns() []int64 {
	// Needs to return an empty slice in case there are no campaigns.
	m.pipesMut.RLock()
	defer m.pipesMut.RUnlock()

	ids := make([]int64, 0, len(m.pipes))
	for _, p := range m.pipes {
		ids = append(ids, int64(p.camp.ID))
	}

	return ids
}

// trackLink register a URL and return its UUID to be used in message templates
// for tracking links.
func (m *Manager) trackLink(url, campUUID, subUUID string) string {
	if m.cfg.DisableTracking {
		return url
	}

	url = strings.ReplaceAll(url, "&amp;", "&")

	cacheKey := campUUID + "\x00" + url
	m.linksMut.RLock()
	if uu, ok := m.links[cacheKey]; ok {
		m.linksMut.RUnlock()
		return fmt.Sprintf(m.cfg.LinkTrackURL, uu, campUUID, subUUID)
	}
	m.linksMut.RUnlock()

	// Register link.
	uu, err := m.store.CreateLink(campUUID, url)
	if err != nil {
		m.log.Printf("error registering tracking for link '%s': %v", url, err)

		// If the registration fails, fail over to the original URL.
		return url
	}

	m.linksMut.Lock()
	m.links[cacheKey] = uu
	m.linksMut.Unlock()

	return fmt.Sprintf(m.cfg.LinkTrackURL, uu, campUUID, subUUID)
}

// sendNotif sends a notification to registered admin e-mails.
func (m *Manager) sendNotif(c *models.Campaign, status, reason string) error {
	var (
		subject = fmt.Sprintf("%s: %s", cases.Title(language.Und).String(status), c.Name)
		data    = map[string]any{
			"ID":     c.ID,
			"Name":   c.Name,
			"Status": status,
			"Sent":   c.Sent,
			"ToSend": c.ToSend,
			"Reason": reason,
		}
	)

	return m.fnNotify(subject, data)
}

// makeGnericFuncMap returns a generic template func map with custom template
// functions and sprig template functions.
func (m *Manager) makeGnericFuncMap() template.FuncMap {
	funcs := template.FuncMap{
		"Date": func(layout string) string {
			if layout == "" {
				layout = time.ANSIC
			}
			return time.Now().Format(layout)
		},
		"L": func() *i18n.I18n {
			return m.i18n
		},
		"Safe": func(safeHTML string) template.HTML {
			return template.HTML(safeHTML)
		},
	}

	// Copy spring functions.
	sprigFuncs := sprig.GenericFuncMap()
	delete(sprigFuncs, "env")
	delete(sprigFuncs, "expandenv")
	delete(sprigFuncs, "getHostByName")

	maps.Copy(funcs, sprigFuncs)

	return funcs
}

// GetMediaAttachments loads media blobs and returns them as message attachments.
func (m *Manager) GetMediaAttachments(mediaIDs []int64) ([]models.Attachment, error) {
	attachments := make([]models.Attachment, 0, len(mediaIDs))
	seen := make(map[int64]struct{}, len(mediaIDs))
	for _, mid := range mediaIDs {
		if mid < 1 {
			continue
		}
		if _, ok := seen[mid]; ok {
			continue
		}
		seen[mid] = struct{}{}

		a, err := m.store.GetAttachment(int(mid))
		if err != nil {
			return nil, fmt.Errorf("error fetching attachment %d: %w", mid, err)
		}
		a.MediaID = int(mid)

		attachments = append(attachments, a)
	}

	return attachments, nil
}

// attachMedia loads any media/attachments from the media store and attaches
// the byte blobs to the campaign.
func (m *Manager) attachMedia(c *models.Campaign) error {
	if c == nil {
		return errors.New("campaign is nil")
	}
	if len(c.Attachments) > 0 {
		return nil
	}

	var (
		attachments []models.Attachment
		err         error
	)
	if scoped, ok := m.store.(CampaignAttachmentStore); ok {
		attachments, err = scoped.GetCampaignAttachments(c, []int64(c.MediaIDs))
	} else {
		attachments, err = m.GetMediaAttachments([]int64(c.MediaIDs))
	}
	if err != nil {
		return fmt.Errorf("error fetching attachments on campaign %s: %w", c.Name, err)
	}
	c.Attachments = attachments

	return nil
}

// MakeAttachmentHeader is a helper function that returns a
// textproto.MIMEHeader tailored for attachments, primarily
// email. If no encoding is given, base64 is assumed.
func MakeAttachmentHeader(filename, encoding, contentType string) textproto.MIMEHeader {
	if encoding == "" {
		encoding = "base64"
	}

	// Do not allow a user-provided filename to create additional MIME headers.
	filename = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(filename, "\r", ""), "\n", ""))
	if filename == "" {
		filename = "attachment"
	}

	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType == "" {
		mediaType = "application/octet-stream"
		params = map[string]string{}
	}
	params["name"] = filename
	contentType = mime.FormatMediaType(mediaType, params)
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	h.Set("Content-Type", contentType)
	h.Set("Content-Transfer-Encoding", encoding)
	return h
}
