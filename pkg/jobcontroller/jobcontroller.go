// Copyright 2025, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package jobcontroller

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wavetermdev/waveterm/pkg/blocklogger"
	"github.com/wavetermdev/waveterm/pkg/filestore"
	"github.com/wavetermdev/waveterm/pkg/panichandler"
	"github.com/wavetermdev/waveterm/pkg/remote/conncontroller"
	"github.com/wavetermdev/waveterm/pkg/streamclient"
	"github.com/wavetermdev/waveterm/pkg/telemetry"
	"github.com/wavetermdev/waveterm/pkg/telemetry/telemetrydata"
	"github.com/wavetermdev/waveterm/pkg/util/ds"
	"github.com/wavetermdev/waveterm/pkg/util/envutil"
	"github.com/wavetermdev/waveterm/pkg/util/shellutil"
	"github.com/wavetermdev/waveterm/pkg/util/utilfn"
	"github.com/wavetermdev/waveterm/pkg/utilds"
	"github.com/wavetermdev/waveterm/pkg/wavebase"
	"github.com/wavetermdev/waveterm/pkg/wavejwt"
	"github.com/wavetermdev/waveterm/pkg/waveobj"
	"github.com/wavetermdev/waveterm/pkg/wconfig"
	"github.com/wavetermdev/waveterm/pkg/wcore"
	"github.com/wavetermdev/waveterm/pkg/wps"
	"github.com/wavetermdev/waveterm/pkg/wshrpc"
	"github.com/wavetermdev/waveterm/pkg/wshrpc/wshclient"
	"github.com/wavetermdev/waveterm/pkg/wshutil"
	"github.com/wavetermdev/waveterm/pkg/wstore"
	"golang.org/x/sync/singleflight"
)

const DefaultTimeout = 2 * time.Second

const (
	JobManagerStatus_Init    = "init"
	JobManagerStatus_Running = "running"
	JobManagerStatus_Done    = "done"
)

const (
	JobDoneReason_StartupError = "startuperror"
	JobDoneReason_Gone         = "gone"
	JobDoneReason_Terminated   = "terminated"
)

const (
	JobConnStatus_Disconnected = "disconnected"
	JobConnStatus_Connecting   = "connecting"
	JobConnStatus_Connected    = "connected"
)

const (
	JobKind_Shell = "shell"
	JobKind_Task  = "task"
)

const DefaultStreamRwnd = 64 * 1024
const MetaKey_TotalGap = "totalgap"
const JobOutputFileName = "term"
const AutoReconnectDelay = 1 * time.Second
const AutoReconnectCooldown = 30 * time.Second

// OldStreamQuiesceTimeout is how long restartStreaming waits for a graceful
// drain of the old output loop before failing the reconnect.
const OldStreamQuiesceTimeout = 5 * time.Second

// StreamRecoveryAttemptTimeout covers quiesce + route wait + Prepare + Start.
const StreamRecoveryAttemptTimeout = 25 * time.Second

// Stream recovery backoff after Prepare/Start/stream loss without a new route-down.
const (
	StreamRecoveryBaseDelay = 1 * time.Second
	StreamRecoveryMaxDelay  = 16 * time.Second
	StreamRecoveryMaxTries  = 6
)

// Persistence / quiesce / recovery sentinels — callers must not treat channel-close alone as success.
var (
	ErrStreamQuiesceTimeout   = errors.New("stream quiesce timeout")
	ErrStreamPersistence      = errors.New("stream persistence failed")
	ErrStreamMirrorPending    = errors.New("stream mirror repair pending")
	ErrStreamInvariant        = errors.New("stream generation invariant violated")
	ErrJobDeleted             = errors.New("job deleted")
	ErrJobManagerGone         = errors.New("job manager has exited")
	ErrGenerationExists       = errors.New("stream generation already exists")
	ErrCandidateNotPromotable = errors.New("candidate generation not promotable")
)

// Max rapid durable-append retries before switching to slow cancellable backoff.
// Exhausting this budget does NOT seal the generation permanently — pending is
// re-driven until success or generation cancel (DeleteJob).
const StreamPersistMaxAttempts = 12

// Cap on in-memory mirror outbox. Overflow marks mirror dirty for repair without
// unbounded growth; primary is never re-run to fix mirrors.
const StreamMirrorOutboxMax = 1 * 1024 * 1024

// Slow persistence backoff after fast budget (cancellable via gen.ctx).
var streamPersistSlowDelays = []time.Duration{
	5 * time.Second,
	10 * time.Second,
	30 * time.Second,
}

type streamPhase int

const (
	streamPhaseCandidate streamPhase = iota
	streamPhaseActive
	streamPhaseQuiescing
	streamPhaseFailedPersisting // primary or terminal reconcile outstanding
)

// loopOutcome records why the output loop stopped (before Start resolution for candidates).
type loopOutcome int

const (
	loopOutcomeRunning loopOutcome = iota
	loopOutcomeRemoteTerminal
	loopOutcomeTransportLost
	loopOutcomePersistenceFailed
	loopOutcomeDrained
	loopOutcomeSuperseded
	loopOutcomeCanceled
)

// streamGeneration is the controller-owned owner for one durable output install.
// Lifecycle: candidate -> active -> quiescing -> removed
//
//	\-> failed-persisting (keep reader/pending; forbid WaveFS snapshot)
//
// Only phaseActive with a live owner, no resultErr, and no terminalPend may
// publish Connected / pass CheckJobConnected.
// Only phaseActive may mark StreamDone from a terminal remote event.
// Map removal is always compare-and-delete by generation pointer.
//
// terminalObserved vs terminalPend: a candidate may observe EOF/error before
// Start resolves; terminalPend (DB StreamDone still required) is only set after
// promote. Unpromoted deferred terminals must never seal irreversible Result.
type streamGeneration struct {
	id     string
	reader *streamclient.Reader
	ctx    context.Context
	cancel context.CancelFunc

	mu            sync.Mutex
	persistGateMu sync.Mutex
	persistGate   chan struct{} // cancellable single-owner primary append gate
	phase         streamPhase
	pending       []byte // durable bytes Read but not yet committed to WaveFS primary
	mirrorPending []byte // primary-committed bytes not yet mirrored (degraded path)
	mirrorWake    chan struct{}
	mirrorSpace   chan struct{}
	mirrorDone    chan struct{}
	mirrorRunning bool
	mirrorLastErr error
	mirrorInvErr  error
	done          chan struct{}
	resultErr     error
	terminalObs   bool // remote EOF/error observed (may predate promote)
	terminalEOF   bool
	terminalErr   error
	terminalPend  bool // StreamDone DB commit still needed (promoted only)
	outcome       loopOutcome
	startOK       bool // Start RPC succeeded (promote path)
	startDone     bool // Start RPC path finished (success or fail)
	startWait     chan struct{}
	finishOnce    sync.Once
	loopExited    bool // output loop returned (owner no longer consuming)
}

// activeJobStream is retained as an alias for older call sites/tests.
type activeJobStream = streamGeneration

type connState struct {
	actual      bool
	processed   bool
	reconciling bool
}

type connStateManager struct {
	sync.Mutex
	m           map[string]*connState
	reconcileCh chan struct{}
}

type jobState struct {
	stateLock       sync.Mutex
	isConnecting    bool
	connectedStatus string
}

var (
	jobConnStates         = make(map[string]string)
	jobControllerLock     sync.Mutex
	blockJobStatusVersion utilds.VersionTs

	connStates = &connStateManager{
		m:           make(map[string]*connState),
		reconcileCh: make(chan struct{}, 1),
	}

	// activeJobStreams maps jobId → current stream generation (candidate or active).
	activeJobStreams = ds.MakeSyncMap[*streamGeneration]()

	jobTerminationMessageWritten = ds.MakeSyncMap[bool]()

	lastAutoReconnectAttempt = ds.MakeSyncMap[int64]()

	reconnectGroup           singleflight.Group
	terminateJobManagerGroup singleflight.Group

	// Injected append/sleep hooks for tests (production defaults = WaveFS + time.Sleep).
	appendDurableJobOutputFn = appendDurableJobOutput
	appendBlockMirrorFn      = appendBlockOutputMirror
	commitStreamDoneFn       = commitStreamDone
	streamAppendRetrySleep   = time.Sleep
)

// streamRecoveryOwner is the single recovery coordinator per job.
// Only external route/connection/manual events may interrupt backoff.
// doReconnectJob must NOT re-request recovery into the same running owner.
//
// requestVersion is monotonic: each external request bumps it. The owner loop
// records seenVersion when it begins work. Exit at max attempts only when
// requestVersion == seenVersion (no newer external event). doneCh is created
// once per map entry and never replaced, so cancel always waits the real run.
type streamRecoveryOwner struct {
	mu             sync.Mutex
	running        bool
	attempt        int
	connName       string
	cause          string
	requestVersion int64
	seenVersion    int64
	waitingSSH     bool
	wakeCh         chan struct{} // external wakes only
	stopCh         chan struct{}
	attemptCancel  context.CancelFunc
	doneCh         chan struct{} // closed once when owner fully exits (never replaced)
}

type streamRecoveryAttemptState int

const (
	recoveryAttemptStopped streamRecoveryAttemptState = iota
	recoveryAttemptExhausted
	recoveryAttemptReady
	recoveryAttemptBudgetReset
)

type streamRecoveryWork struct {
	attempt  int
	version  int64
	connName string
	cause    string
}

var streamRecoveryOwners = struct {
	sync.Mutex
	m map[string]*streamRecoveryOwner
}{m: make(map[string]*streamRecoveryOwner)}

// jobLifecycle tracks per-job route/init/delete state under one lock so
// route-up, first install, Connected publish, and DeleteJob cannot race.
type jobLifecycle struct {
	mu           sync.Mutex
	routeUp      bool
	routeEpoch   uint64
	initializing bool // true from job create until first Connected publish
	deleting     bool
	deleted      bool
}

var jobLifecycles = struct {
	sync.Mutex
	m map[string]*jobLifecycle
}{m: make(map[string]*jobLifecycle)}

// Append retry schedule (fast budget). After StreamPersistMaxAttempts, production
// switches to streamPersistSlowDelays without sealing permanent failure.
var streamAppendRetryDelays = []time.Duration{
	100 * time.Millisecond,
	250 * time.Millisecond,
	500 * time.Millisecond,
	1 * time.Second,
	2 * time.Second,
	5 * time.Second,
}

// Injected sleep for recovery backoff tests (production = time.Sleep).
var streamRecoverySleep = time.Sleep

func getOrCreateJobLifecycle(jobId string) *jobLifecycle {
	jobLifecycles.Lock()
	defer jobLifecycles.Unlock()
	lc, ok := jobLifecycles.m[jobId]
	if !ok {
		lc = &jobLifecycle{}
		jobLifecycles.m[jobId] = lc
	}
	return lc
}

func beginJobInitializing(jobId string) {
	lc := getOrCreateJobLifecycle(jobId)
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.routeUp = false
	lc.routeEpoch++
	lc.initializing = true
	lc.deleting = false
	lc.deleted = false
}

func isJobDeletingOrDeleted(jobId string) bool {
	jobLifecycles.Lock()
	lc := jobLifecycles.m[jobId]
	jobLifecycles.Unlock()
	if lc == nil {
		return false
	}
	lc.mu.Lock()
	defer lc.mu.Unlock()
	return lc.deleting || lc.deleted
}

// markJobDeleting sets the tombstone. Returns false if already deleted.
func markJobDeleting(jobId string) bool {
	lc := getOrCreateJobLifecycle(jobId)
	lc.mu.Lock()
	defer lc.mu.Unlock()
	if lc.deleted {
		return false
	}
	lc.deleting = true
	lc.routeUp = false
	lc.routeEpoch++
	SetJobConnStatus(jobId, JobConnStatus_Disconnected)
	return true
}

func markJobDeleted(jobId string) {
	lc := getOrCreateJobLifecycle(jobId)
	lc.mu.Lock()
	lc.deleting = false
	lc.deleted = true
	lc.routeUp = false
	lc.routeEpoch++
	lc.initializing = false
	SetJobConnStatus(jobId, JobConnStatus_Disconnected)
	lc.mu.Unlock()
	// Drop map entry after a short delay is unnecessary; keep tombstone until process end
	// so late recovery cannot recreate. Prune on next insert via beginJobInitializing.
}

func setJobRouteUp(jobId string, up bool) (wasInitializing bool, isDeleting bool) {
	lc := getOrCreateJobLifecycle(jobId)
	lc.mu.Lock()
	defer lc.mu.Unlock()
	if lc.deleting || lc.deleted {
		return lc.initializing, true
	}
	lc.routeUp = up
	lc.routeEpoch++
	if !up {
		// Route-down and Disconnected are one linearized transition. Any later
		// Connected publisher takes lc.mu and must observe routeUp=false.
		SetJobConnStatus(jobId, JobConnStatus_Disconnected)
	}
	return lc.initializing, false
}

func getJobRouteEpoch(jobId string) uint64 {
	lc := getOrCreateJobLifecycle(jobId)
	lc.mu.Lock()
	defer lc.mu.Unlock()
	return lc.routeEpoch
}

// confirmJobRouteUp records a successful router probe only when no route event
// happened since observedEpoch. A concurrent route-down therefore wins over a
// stale WaitForRegister result.
func confirmJobRouteUp(jobId string, observedEpoch uint64) bool {
	lc := getOrCreateJobLifecycle(jobId)
	lc.mu.Lock()
	defer lc.mu.Unlock()
	if lc.deleting || lc.deleted {
		return false
	}
	if lc.routeEpoch != observedEpoch {
		return lc.routeUp
	}
	lc.routeUp = true
	lc.routeEpoch++
	return true
}

func isJobRouteUp(jobId string) bool {
	jobLifecycles.Lock()
	lc := jobLifecycles.m[jobId]
	jobLifecycles.Unlock()
	if lc == nil {
		return false
	}
	lc.mu.Lock()
	defer lc.mu.Unlock()
	return lc.routeUp && !lc.deleting && !lc.deleted
}

type connectedPublishEvidence struct {
	generation *streamGeneration
	streamDone bool
}

func captureConnectedPublishEvidence(jobId string) connectedPublishEvidence {
	if gen := getGeneration(jobId); gen != nil {
		return connectedPublishEvidence{generation: gen}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	job, err := wstore.DBGet[*waveobj.Job](ctx, jobId)
	return connectedPublishEvidence{streamDone: err == nil && job != nil && job.StreamDone}
}

func connectedPublishEvidenceStillValid(jobId string, evidence connectedPublishEvidence) bool {
	if evidence.streamDone {
		return true
	}
	gen := evidence.generation
	if gen == nil {
		return false
	}
	gen.mu.Lock()
	defer gen.mu.Unlock()
	// removeGenerationIf takes gen.mu before deleting, so pointer identity and
	// health remain stable until Connected is written by the caller.
	return getGeneration(jobId) == gen && isGenerationHealthyLocked(gen)
}

// publishConnectedIfAllowed checks lifecycle and stream health, then writes
// Connected while both states are stable against route-down and output failure.
func publishConnectedIfAllowed(jobId string) bool {
	evidence := captureConnectedPublishEvidence(jobId)
	lc := getOrCreateJobLifecycle(jobId)
	lc.mu.Lock()
	defer lc.mu.Unlock()
	if lc.deleting || lc.deleted || lc.initializing || !lc.routeUp ||
		!connectedPublishEvidenceStillValid(jobId, evidence) {
		return false
	}
	SetJobConnStatus(jobId, JobConnStatus_Connected)
	return true
}

// finishJobInitialization closes the first-start handoff and publishes
// Connected when route-up was recorded while initialization was in progress.
// Both operations are atomic with route-down/DeleteJob.
func finishJobInitialization(jobId string) bool {
	evidence := captureConnectedPublishEvidence(jobId)
	lc := getOrCreateJobLifecycle(jobId)
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.initializing = false
	if lc.deleting || lc.deleted || !lc.routeUp ||
		!connectedPublishEvidenceStillValid(jobId, evidence) {
		return false
	}
	SetJobConnStatus(jobId, JobConnStatus_Connected)
	return true
}

func clearJobInitializing(jobId string) {
	lc := getOrCreateJobLifecycle(jobId)
	lc.mu.Lock()
	lc.initializing = false
	lc.mu.Unlock()
}

func InitJobController() {
	go connReconcileWorker()
	go jobPruningWorker()

	rpcClient := wshclient.GetBareRpcClient()
	rpcClient.EventListener.On(wps.Event_RouteUp, handleRouteUpEvent)
	rpcClient.EventListener.On(wps.Event_RouteDown, handleRouteDownEvent)
	rpcClient.EventListener.On(wps.Event_ConnChange, handleConnChangeEvent)
	rpcClient.EventListener.On(wps.Event_BlockClose, handleBlockCloseEvent)
	wshclient.EventSubCommand(rpcClient, wps.SubscriptionRequest{
		Event:     wps.Event_RouteUp,
		AllScopes: true,
	}, nil)
	wshclient.EventSubCommand(rpcClient, wps.SubscriptionRequest{
		Event:     wps.Event_RouteDown,
		AllScopes: true,
	}, nil)
	wshclient.EventSubCommand(rpcClient, wps.SubscriptionRequest{
		Event:     wps.Event_ConnChange,
		AllScopes: true,
	}, nil)
	wshclient.EventSubCommand(rpcClient, wps.SubscriptionRequest{
		Event:     wps.Event_BlockClose,
		AllScopes: true,
	}, nil)
}

func isJobManagerRunning(job *waveobj.Job) bool {
	return job.JobManagerStatus == JobManagerStatus_Running
}

func GetJobManagerStatus(ctx context.Context, jobId string) (string, error) {
	job, err := wstore.DBGet[*waveobj.Job](ctx, jobId)
	if err != nil {
		return "", fmt.Errorf("failed to get job: %w", err)
	}
	if job == nil {
		return JobManagerStatus_Done, nil
	}
	return job.JobManagerStatus, nil
}

func GetAllJobManagerStatus(ctx context.Context) ([]*wshrpc.JobManagerStatusUpdate, error) {
	allJobs, err := wstore.DBGetAllObjsByType[*waveobj.Job](ctx, waveobj.OType_Job)
	if err != nil {
		return nil, fmt.Errorf("failed to get jobs: %w", err)
	}

	var statuses []*wshrpc.JobManagerStatusUpdate
	for _, job := range allJobs {
		statuses = append(statuses, &wshrpc.JobManagerStatusUpdate{
			JobId:            job.OID,
			JobManagerStatus: job.JobManagerStatus,
		})
	}

	return statuses, nil
}

func GetBlockJobStatus(ctx context.Context, blockId string) (*wshrpc.BlockJobStatusData, error) {
	block, err := wstore.DBGet[*waveobj.Block](ctx, blockId)
	if err != nil {
		return nil, fmt.Errorf("failed to get block: %w", err)
	}
	if block == nil {
		return nil, fmt.Errorf("block not found: %s", blockId)
	}

	data := &wshrpc.BlockJobStatusData{
		BlockId:   blockId,
		VersionTs: blockJobStatusVersion.GetVersionTs(),
	}

	if block.JobId == "" {
		return data, nil
	}

	job, err := wstore.DBGet[*waveobj.Job](ctx, block.JobId)
	if err != nil {
		return nil, fmt.Errorf("failed to get job: %w", err)
	}
	if job == nil {
		return data, nil
	}

	data.JobId = job.OID
	data.DoneReason = job.JobManagerDoneReason
	data.StartupError = job.JobManagerStartupError
	data.CmdExitTs = job.CmdExitTs
	data.CmdExitCode = job.CmdExitCode
	data.CmdExitSignal = job.CmdExitSignal

	if job.JobManagerStatus == JobManagerStatus_Init {
		data.Status = "init"
	} else if job.JobManagerStatus == JobManagerStatus_Done {
		data.Status = "done"
	} else if job.JobManagerStatus == JobManagerStatus_Running {
		connStatus := GetJobConnStatus(job.OID)
		if connStatus == JobConnStatus_Connected {
			data.Status = "connected"
		} else {
			data.Status = "disconnected"
		}
	}

	return data, nil
}

func SendBlockJobStatusEvent(ctx context.Context, blockId string) {
	data, err := GetBlockJobStatus(ctx, blockId)
	if err != nil {
		log.Printf("[block:%s] error getting block job status: %v", blockId, err)
		return
	}
	wps.Broker.Publish(wps.WaveEvent{
		Event:  wps.Event_BlockJobStatus,
		Scopes: []string{fmt.Sprintf("block:%s", blockId)},
		Data:   data,
	})
}

func sendBlockJobStatusEventByJob(ctx context.Context, job *waveobj.Job) {
	if job == nil || job.AttachedBlockId == "" {
		return
	}
	SendBlockJobStatusEvent(ctx, job.AttachedBlockId)
}

func connReconcileWorker() {
	defer func() {
		panichandler.PanicHandler("jobcontroller:connReconcileWorker", recover())
	}()

	for range connStates.reconcileCh {
		reconcileAllConns()
	}
}

func reconcileAllConns() {
	connStates.Lock()
	defer connStates.Unlock()

	for connName, cs := range connStates.m {
		if cs.reconciling || cs.actual == cs.processed {
			continue
		}

		cs.reconciling = true
		actual := cs.actual
		go reconcileConn(connName, actual)
	}
}

func reconcileConn(connName string, targetState bool) {
	defer func() {
		panichandler.PanicHandler("jobcontroller:reconcileConn", recover())
	}()

	if targetState {
		onConnectionUp(connName)
	} else {
		onConnectionDown(connName)
	}

	connStates.Lock()
	defer connStates.Unlock()
	if cs, exists := connStates.m[connName]; exists {
		cs.processed = targetState
		cs.reconciling = false
	}

	select {
	case connStates.reconcileCh <- struct{}{}:
	default:
	}
}

func getMetaInt64(meta wshrpc.FileMeta, key string) int64 {
	val, ok := meta[key]
	if !ok {
		return 0
	}
	if intVal, ok := val.(int64); ok {
		return intVal
	}
	if floatVal, ok := val.(float64); ok {
		return int64(floatVal)
	}
	return 0
}

func jobPruningWorker() {
	defer func() {
		panichandler.PanicHandler("jobcontroller:jobPruningWorker", recover())
	}()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	var previousCandidates []string
	for range ticker.C {
		previousCandidates = pruneUnusedJobs(previousCandidates)
	}
}

func pruneUnusedJobs(previousCandidates []string) []string {
	ctx, cancelFn := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelFn()

	allJobs, err := wstore.DBGetAllObjsByType[*waveobj.Job](ctx, waveobj.OType_Job)
	if err != nil {
		log.Printf("[jobpruner] error getting all jobs: %v", err)
		return previousCandidates
	}

	var currentCandidates []string
	for _, job := range allJobs {
		if job.JobManagerStatus == JobManagerStatus_Done && job.AttachedBlockId == "" {
			currentCandidates = append(currentCandidates, job.OID)
		}
	}

	jobsToDelete := utilfn.StrSetIntersection(previousCandidates, currentCandidates)
	if len(previousCandidates) > 0 || len(currentCandidates) > 0 {
		log.Printf("[jobpruner] prev=%d current=%d deleting=%d", len(previousCandidates), len(currentCandidates), len(jobsToDelete))
	}

	for _, jobId := range jobsToDelete {
		err := DeleteJob(ctx, jobId)
		if err != nil {
			log.Printf("[jobpruner] error deleting job %s: %v", jobId, err)
		}
	}

	return currentCandidates
}

func handleRouteUpEvent(event *wps.WaveEvent) {
	handleRouteEvent(event, JobConnStatus_Connected)
}

func handleRouteDownEvent(event *wps.WaveEvent) {
	handleRouteEvent(event, JobConnStatus_Disconnected)
}

func handleRouteEvent(event *wps.WaveEvent, newStatus string) {
	ctx := context.Background()
	for _, scope := range event.Scopes {
		if strings.HasPrefix(scope, "job:") {
			jobId := strings.TrimPrefix(scope, "job:")
			job, err := wstore.DBGet[*waveobj.Job](ctx, jobId)
			if err != nil {
				log.Printf("[job:%s] error getting job for status event: %v", jobId, err)
				continue
			}

			if newStatus == JobConnStatus_Connected {
				// route-up is NOT streamHealthy. Record routeUp under lifecycle;
				// do not publish Connected. During initializing (first start),
				// only record — never start recovery (would race installActive).
				wasInit, deleting := setJobRouteUp(jobId, true)
				if deleting {
					log.Printf("[job:%s] job route up ignored (deleting/deleted)", jobId)
					continue
				}
				log.Printf("[job:%s] job route up recorded (initializing=%v) — not publishing Connected", jobId, wasInit)
				if wasInit {
					// First-start owns install + Connected; route-up only records.
					continue
				}
				if job != nil && isJobManagerRunning(job) {
					// Already Running + active healthy: publish Connected now that route is up
					// (covers first-start where install preceded route-up event).
					if isHealthyActiveGeneration(jobId) {
						if publishConnectedIfAllowed(jobId) {
							if !isJobDeletingOrDeleted(jobId) && isJobRouteUp(jobId) {
								sendBlockJobStatusEventByJob(ctx, job)
								log.Printf("[job:%s] route-up: healthy active stream → Connected", jobId)
							} else {
								SetJobConnStatus(jobId, JobConnStatus_Disconnected)
							}
						}
						continue
					}
					requestStreamRecovery(jobId, job.Connection, "route-up", true)
				}
				continue
			}

			// route-down: clear routeUp and Connected in one conceptual transition
			// so UI cannot stay stale-connected while route is gone.
			_, deleting := setJobRouteUp(jobId, false)
			if !deleting {
				log.Printf("[job:%s] connection status changed to %s", jobId, newStatus)
				sendBlockJobStatusEventByJob(ctx, job)
			}

			if job != nil && isJobManagerRunning(job) && !isJobDeletingOrDeleted(jobId) {
				if shouldAttemptAutoReconnect(jobId) {
					go attemptAutoReconnect(jobId, job.Connection)
				}
			}
		}
	}
}

func shouldAttemptAutoReconnect(jobId string) bool {
	now := time.Now().Unix()
	lastAttempt, exists := lastAutoReconnectAttempt.GetEx(jobId)

	if !exists {
		lastAutoReconnectAttempt.Set(jobId, now)
		return true
	}

	timeSinceLastAttempt := time.Duration(now-lastAttempt) * time.Second
	if timeSinceLastAttempt >= AutoReconnectCooldown {
		lastAutoReconnectAttempt.Set(jobId, now)
		return true
	}

	return false
}

func attemptAutoReconnect(jobId string, connName string) {
	defer func() {
		panichandler.PanicHandler("jobcontroller:attemptAutoReconnect", recover())
	}()

	time.Sleep(AutoReconnectDelay)

	isConnected, err := conncontroller.IsConnected(connName)
	if err != nil || !isConnected {
		log.Printf("[job:%s] connection %s is down, skipping auto-reconnect", jobId, connName)
		return
	}

	log.Printf("[job:%s] connection %s still up after route down, submitting recovery", jobId, connName)
	requestStreamRecovery(jobId, connName, "route-down", true)
}

// streamRecoveryBackoff returns delay for attempt (1-based), capped.
func streamRecoveryBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	// 1s, 2s, 4s, 8s, 16s, 16s...
	shift := attempt - 1
	if shift > 4 {
		shift = 4
	}
	delay := StreamRecoveryBaseDelay << shift
	if delay > StreamRecoveryMaxDelay {
		delay = StreamRecoveryMaxDelay
	}
	return delay
}

// scheduleStreamRecovery is the legacy entrypoint used by tests; forwards to the
// single recovery coordinator (attempt number is owned by the coordinator).
func scheduleStreamRecovery(jobId string, connName string, attempt int) {
	_ = attempt
	requestStreamRecovery(jobId, connName, "schedule", false)
}

// requestStreamRecovery submits recovery work to the per-job owner. Duplicate
// external events only update cause/conn and bump requestVersion; they do not
// spawn additional retry trees. Internal doReconnectJob failures must NOT call
// this (owner loop is the sole retry owner).
func requestStreamRecovery(jobId string, connName string, cause string, immediate bool) {
	if isJobDeletingOrDeleted(jobId) {
		log.Printf("[job:%s] stream recovery ignored (deleting/deleted) cause=%s", jobId, cause)
		return
	}
	streamRecoveryOwners.Lock()
	owner, ok := streamRecoveryOwners.m[jobId]
	if !ok {
		owner = &streamRecoveryOwner{
			wakeCh: make(chan struct{}, 1),
			stopCh: make(chan struct{}),
			doneCh: make(chan struct{}),
		}
		streamRecoveryOwners.m[jobId] = owner
	}
	owner.mu.Lock()
	owner.connName = connName
	owner.cause = cause
	owner.requestVersion++
	requestVersion := owner.requestVersion
	alreadyRunning := owner.running
	if alreadyRunning {
		// External interrupt of backoff / exit window — version already bumped.
		owner.mu.Unlock()
		streamRecoveryOwners.Unlock()
		select {
		case owner.wakeCh <- struct{}{}:
		default:
		}
		log.Printf("[job:%s] stream recovery request merged (cause=%s ver=%d)", jobId, cause, requestVersion)
		return
	}
	owner.running = true
	owner.attempt = 0
	// doneCh is created once with the owner; never replaced while map entry lives.
	owner.mu.Unlock()
	streamRecoveryOwners.Unlock()

	go runStreamRecovery(jobId, owner, immediate)
}

// wakeStreamRecovery wakes a recovery owner that is waiting on SSH or sleeping.
func wakeStreamRecovery(jobId string) {
	if isJobDeletingOrDeleted(jobId) {
		return
	}
	streamRecoveryOwners.Lock()
	owner := streamRecoveryOwners.m[jobId]
	streamRecoveryOwners.Unlock()
	if owner == nil {
		return
	}
	owner.mu.Lock()
	owner.requestVersion++
	owner.mu.Unlock()
	select {
	case owner.wakeCh <- struct{}{}:
	default:
	}
}

// cancelStreamRecovery stops further retries, cancels the in-flight attempt,
// and waits for the owner goroutine to exit. A timeout retains the map owner so
// DeleteJob cannot mistake an unjoined writer for a completed cancellation.
func cancelStreamRecovery(ctx context.Context, jobId string) error {
	streamRecoveryOwners.Lock()
	owner := streamRecoveryOwners.m[jobId]
	if owner == nil {
		streamRecoveryOwners.Unlock()
		return nil
	}
	owner.mu.Lock()
	select {
	case <-owner.stopCh:
	default:
		close(owner.stopCh)
	}
	if owner.attemptCancel != nil {
		owner.attemptCancel()
	}
	doneCh := owner.doneCh
	// Align seen with request so exit will not restart for a version we canceled.
	owner.seenVersion = owner.requestVersion
	owner.mu.Unlock()
	// Keep map entry until clearStreamRecoveryOwner so exit-window races see stopCh.
	streamRecoveryOwners.Unlock()

	if doneCh != nil {
		select {
		case <-doneCh:
		case <-ctx.Done():
			return fmt.Errorf("cancel stream recovery for job %s: %w", jobId, ctx.Err())
		}
	}
	streamRecoveryOwners.Lock()
	if streamRecoveryOwners.m[jobId] == owner {
		delete(streamRecoveryOwners.m, jobId)
	}
	streamRecoveryOwners.Unlock()
	return nil
}

// clearStreamRecoveryOwner finishes the owner goroutine. Exit and version
// check are under the same lock so a request in the exit window is not lost.
// doneCh is never replaced — only closed once when the owner truly ends.
func clearStreamRecoveryOwner(jobId string, owner *streamRecoveryOwner) {
	streamRecoveryOwners.Lock()
	owner.mu.Lock()
	// Newer external request than what this run has processed?
	if owner.requestVersion > owner.seenVersion {
		select {
		case <-owner.stopCh:
			// Canceled — do not restart.
			owner.running = false
			owner.mu.Unlock()
			if streamRecoveryOwners.m[jobId] == owner {
				delete(streamRecoveryOwners.m, jobId)
			}
			streamRecoveryOwners.Unlock()
			close(owner.doneCh)
			return
		default:
		}
		owner.attempt = 0
		owner.running = true
		cause := owner.cause
		owner.mu.Unlock()
		streamRecoveryOwners.Unlock()
		log.Printf("[job:%s] stream recovery restarting after newer request (cause=%s)", jobId, cause)
		// Keep the same doneCh — cancel waiters stay blocked until true exit.
		go runStreamRecovery(jobId, owner, true)
		return
	}
	owner.running = false
	if streamRecoveryOwners.m[jobId] == owner {
		delete(streamRecoveryOwners.m, jobId)
	}
	doneCh := owner.doneCh
	owner.mu.Unlock()
	streamRecoveryOwners.Unlock()
	if doneCh != nil {
		close(doneCh)
	}
}

// beginStreamRecoveryAttempt allocates one work unit. requestVersion is only
// considered seen after completeStreamRecoveryAttempt, so an event arriving
// during attempt 6 resets the budget instead of being consumed at the boundary.
func beginStreamRecoveryAttempt(owner *streamRecoveryOwner) (streamRecoveryWork, streamRecoveryAttemptState) {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	select {
	case <-owner.stopCh:
		return streamRecoveryWork{}, recoveryAttemptStopped
	default:
	}
	select {
	case <-owner.wakeCh:
	default:
	}

	state := recoveryAttemptReady
	if owner.attempt >= StreamRecoveryMaxTries {
		if owner.requestVersion <= owner.seenVersion {
			return streamRecoveryWork{}, recoveryAttemptExhausted
		}
		owner.attempt = 0
		state = recoveryAttemptBudgetReset
	}
	owner.attempt++
	return streamRecoveryWork{
		attempt:  owner.attempt,
		version:  owner.requestVersion,
		connName: owner.connName,
		cause:    owner.cause,
	}, state
}

func completeStreamRecoveryAttempt(owner *streamRecoveryOwner, version int64) {
	owner.mu.Lock()
	if version > owner.seenVersion {
		owner.seenVersion = version
	}
	owner.mu.Unlock()
}

func runStreamRecovery(jobId string, owner *streamRecoveryOwner, immediate bool) {
	defer func() {
		panichandler.PanicHandler("jobcontroller:runStreamRecovery", recover())
		clearStreamRecoveryOwner(jobId, owner)
	}()

	for {
		if isJobDeletingOrDeleted(jobId) {
			return
		}
		work, attemptState := beginStreamRecoveryAttempt(owner)
		if attemptState == recoveryAttemptStopped {
			return
		}
		if attemptState == recoveryAttemptExhausted {
			log.Printf("[job:%s] stream recovery giving up after %d attempts", jobId, StreamRecoveryMaxTries)
			markJobStreamUnhealthy(jobId)
			return
		}
		if attemptState == recoveryAttemptBudgetReset {
			log.Printf("[job:%s] stream recovery budget reset by external request (cause=%s)", jobId, work.cause)
			immediate = true
		}
		attempt := work.attempt
		connName := work.connName
		cause := work.cause

		if !(immediate && attempt == 1) {
			delay := streamRecoveryBackoff(attempt)
			log.Printf("[job:%s] stream recovery attempt %d sleeping %s (cause=%s)", jobId, attempt, delay, cause)
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
			case <-owner.stopCh:
				timer.Stop()
				return
			case <-owner.wakeCh:
				// External event only (connection-up / route-up / manual).
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
			}
		}
		// Only the first attempt of a fresh start is "immediate".
		immediate = false

		if isJobDeletingOrDeleted(jobId) {
			return
		}

		// Healthy: stop.
		hctx, hcancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, herr := CheckJobConnected(hctx, jobId)
		hcancel()
		if herr == nil {
			completeStreamRecoveryAttempt(owner, work.version)
			log.Printf("[job:%s] stream recovery attempt %d skipped: already healthy", jobId, attempt)
			lastAutoReconnectAttempt.Delete(jobId)
			return
		}

		isConnected, connErr := conncontroller.IsConnected(connName)
		if connErr != nil || !isConnected {
			// Do not burn attempts while SSH is down; wait for connection-up.
			completeStreamRecoveryAttempt(owner, work.version)
			log.Printf("[job:%s] stream recovery waiting for SSH (attempt=%d cause=%s)", jobId, attempt, cause)
			owner.mu.Lock()
			owner.waitingSSH = true
			if owner.attempt > 0 {
				owner.attempt--
			}
			owner.mu.Unlock()
			select {
			case <-owner.wakeCh:
				owner.mu.Lock()
				owner.waitingSSH = false
				owner.mu.Unlock()
				continue
			case <-owner.stopCh:
				return
			case <-time.After(5 * time.Minute):
				owner.mu.Lock()
				owner.waitingSSH = false
				owner.mu.Unlock()
				continue
			}
		}

		if GetJobConnStatus(jobId) == JobConnStatus_Connected && !isHealthyActiveGeneration(jobId) {
			markJobStreamUnhealthy(jobId)
		}

		// Re-drive retained pending on a failed-persisting generation before full reconnect.
		if gen := getGeneration(jobId); gen != nil {
			if rederr := redriveGenerationPersistence(context.Background(), jobId, gen); rederr != nil {
				log.Printf("[job:%s] redrive persistence before reconnect: %v", jobId, rederr)
			} else if isHealthyActiveGeneration(jobId) {
				// Still may need Connected publish if only persistence was stuck.
				if GetJobConnStatus(jobId) != JobConnStatus_Connected {
					if publishConnectedIfAllowed(jobId) {
						jctx, jcancel := context.WithTimeout(context.Background(), 2*time.Second)
						job, _ := wstore.DBGet[*waveobj.Job](jctx, jobId)
						if job != nil {
							sendBlockJobStatusEventByJob(jctx, job)
						}
						jcancel()
					}
				}
				hctx2, hcancel2 := context.WithTimeout(context.Background(), 2*time.Second)
				_, herr2 := CheckJobConnected(hctx2, jobId)
				hcancel2()
				if herr2 == nil {
					completeStreamRecoveryAttempt(owner, work.version)
					lastAutoReconnectAttempt.Delete(jobId)
					return
				}
			}
		}

		log.Printf("[job:%s] stream recovery attempt %d starting (cause=%s)", jobId, attempt, cause)
		rctx, rcancel := context.WithTimeout(context.Background(), StreamRecoveryAttemptTimeout)
		owner.mu.Lock()
		owner.attemptCancel = rcancel
		owner.mu.Unlock()
		err := ReconnectJob(rctx, jobId, nil)
		owner.mu.Lock()
		owner.attemptCancel = nil
		owner.mu.Unlock()
		rcancel()
		completeStreamRecoveryAttempt(owner, work.version)
		if err != nil {
			if isTerminalReconnectError(err) {
				log.Printf("[job:%s] stream recovery terminal failure: %v", jobId, err)
				markJobStreamUnhealthy(jobId)
				return
			}
			log.Printf("[job:%s] stream recovery attempt %d failed: %v", jobId, attempt, err)
			// Continue with backoff — do NOT self-signal wakeCh.
			continue
		}
		log.Printf("[job:%s] stream recovery attempt %d succeeded", jobId, attempt)
		lastAutoReconnectAttempt.Delete(jobId)
		return
	}
}

func isTerminalReconnectError(err error) bool {
	if err == nil {
		return false
	}
	// Typed/sentinel only — never bare substring "not found".
	return errors.Is(err, ErrJobDeleted) ||
		errors.Is(err, ErrJobManagerGone) ||
		errors.Is(err, wstore.ErrNotFound)
}

// getActiveStreamId returns the stream id of the current generation (any phase), or "".
func getActiveStreamId(jobId string) string {
	ajs, ok := activeJobStreams.GetEx(jobId)
	if !ok || ajs == nil {
		return ""
	}
	return ajs.id
}

// isCurrentGeneration reports whether streamId is the map's current generation.
func isCurrentGeneration(jobId string, streamId string) bool {
	return getActiveStreamId(jobId) == streamId
}

func isGenerationHealthyLocked(ajs *streamGeneration) bool {
	if ajs.phase != streamPhaseActive {
		return false
	}
	if ajs.resultErr != nil || ajs.terminalPend {
		return false
	}
	switch ajs.outcome {
	case loopOutcomeRunning:
		return !ajs.loopExited
	case loopOutcomeRemoteTerminal:
		// Active remote-terminal is healthy only after StreamDone commit cleared
		// terminalPend; terminalObs distinguishes it from fabricated state.
		return ajs.terminalObs
	default:
		return false
	}
}

// isHealthyActiveGeneration is true only for the map's current promoted
// generation while its output owner is live, or after durable remote terminal.
func isHealthyActiveGeneration(jobId string) bool {
	ajs := getGeneration(jobId)
	if ajs == nil {
		return false
	}
	ajs.mu.Lock()
	defer ajs.mu.Unlock()
	return getGeneration(jobId) == ajs && isGenerationHealthyLocked(ajs)
}

// hasActiveJobStream reports whether a healthy active (promoted) reader is installed.
func hasActiveJobStream(jobId string) bool {
	return isHealthyActiveGeneration(jobId)
}

// hasAnyJobStreamGeneration reports candidate or active map presence.
func hasAnyJobStreamGeneration(jobId string) bool {
	return getActiveStreamId(jobId) != ""
}

func (gen *streamGeneration) finish(err error) {
	gen.finishOnce.Do(func() {
		gen.mu.Lock()
		// Unpromoted candidate with only observed terminal must NOT seal
		// irreversible ErrStreamPersistence — Start-fail drain clears those flags.
		if err == nil && gen.phase == streamPhaseCandidate && !gen.startOK {
			// Bytes still pending are a real persistence problem.
			if len(gen.pending) > 0 {
				err = fmt.Errorf("%w: %d bytes still pending", ErrStreamPersistence, len(gen.pending))
			}
			// Drop uncommitted terminal requirement for failed Start.
			gen.terminalPend = false
		} else if err == nil && (len(gen.pending) > 0 || gen.terminalPend) {
			if len(gen.pending) > 0 {
				err = fmt.Errorf("%w: %d bytes still pending", ErrStreamPersistence, len(gen.pending))
			} else {
				err = fmt.Errorf("%w: terminal StreamDone not committed", ErrStreamPersistence)
			}
		}
		// Any durable failure (bytes or terminal) enters failed-persisting so health fails.
		if err != nil && (len(gen.pending) > 0 || gen.terminalPend || errors.Is(err, ErrStreamPersistence)) {
			gen.phase = streamPhaseFailedPersisting
		}
		gen.resultErr = err
		gen.loopExited = true
		gen.mu.Unlock()
		if gen.done != nil {
			close(gen.done)
		}
	})
}

// Result returns the persistence outcome after done is closed.
func (gen *streamGeneration) Result() error {
	if gen == nil || gen.done == nil {
		return nil
	}
	<-gen.done
	gen.mu.Lock()
	defer gen.mu.Unlock()
	return gen.resultErr
}

// PendingLen returns durable-pending byte count (for tests/diagnostics).
func (gen *streamGeneration) PendingLen() int {
	if gen == nil {
		return 0
	}
	gen.mu.Lock()
	defer gen.mu.Unlock()
	return len(gen.pending)
}

func (gen *streamGeneration) signalStartResolved(ok bool) {
	if gen == nil {
		return
	}
	gen.mu.Lock()
	if !gen.startDone {
		gen.startDone = true
		gen.startOK = ok
		if gen.startWait != nil {
			close(gen.startWait)
		}
	}
	gen.mu.Unlock()
}

func (gen *streamGeneration) waitStartResolved(ctx context.Context) (ok bool) {
	if gen == nil {
		return false
	}
	gen.mu.Lock()
	if gen.startDone {
		ok = gen.startOK
		gen.mu.Unlock()
		return ok
	}
	ch := gen.startWait
	gen.mu.Unlock()
	select {
	case <-ch:
		gen.mu.Lock()
		ok = gen.startOK
		gen.mu.Unlock()
		return ok
	case <-ctx.Done():
		return false
	}
}

// createCandidate registers a generation before StartStream so ACKed data has a
// persistence owner. Uses SetUnless — any existing generation blocks install.
func createCandidate(jobId string, streamId string, reader *streamclient.Reader) (*streamGeneration, error) {
	if isJobDeletingOrDeleted(jobId) {
		return nil, fmt.Errorf("%w: job is deleting", ErrJobDeleted)
	}
	if cur, ok := activeJobStreams.GetEx(jobId); ok && cur != nil {
		return nil, fmt.Errorf("%w: previous generation %s still present (pending=%d)",
			ErrGenerationExists, cur.id, cur.PendingLen())
	}
	ctx, cancel := context.WithCancel(context.Background())
	gen := &streamGeneration{
		id:        streamId,
		reader:    reader,
		ctx:       ctx,
		cancel:    cancel,
		phase:     streamPhaseCandidate,
		done:      make(chan struct{}),
		startWait: make(chan struct{}),
		outcome:   loopOutcomeRunning,
	}
	if !activeJobStreams.SetUnless(jobId, gen) {
		cancel()
		return nil, fmt.Errorf("%w: concurrent install for job %s", ErrGenerationExists, jobId)
	}
	// Re-check tombstone after install (TOCTOU with DeleteJob).
	if isJobDeletingOrDeleted(jobId) {
		activeJobStreams.DeleteIf(jobId, func(cur *streamGeneration, exists bool) bool {
			return exists && cur == gen
		})
		cancel()
		return nil, fmt.Errorf("%w: job is deleting", ErrJobDeleted)
	}
	return gen, nil
}

// promoteCandidate moves candidate -> active after StartStream succeeds.
// Rejects transport-lost candidates (not healthy).
func promoteCandidate(jobId string, streamId string) error {
	if isJobDeletingOrDeleted(jobId) {
		return fmt.Errorf("%w: job is deleting", ErrJobDeleted)
	}
	gen, ok := activeJobStreams.GetEx(jobId)
	if !ok || gen == nil || gen.id != streamId {
		return fmt.Errorf("%w: no candidate generation %s", ErrCandidateNotPromotable, streamId)
	}
	// Pointer-stable check under map: ensure still this gen.
	if getGeneration(jobId) != gen {
		return fmt.Errorf("%w: generation %s no longer current", ErrCandidateNotPromotable, streamId)
	}
	gen.mu.Lock()
	defer gen.mu.Unlock()
	if gen.phase != streamPhaseCandidate {
		return fmt.Errorf("%w: generation %s is not candidate (phase=%d)", ErrCandidateNotPromotable, streamId, gen.phase)
	}
	if gen.outcome == loopOutcomeTransportLost {
		return fmt.Errorf("%w: candidate saw transport loss before Start", ErrCandidateNotPromotable)
	}
	if gen.outcome == loopOutcomePersistenceFailed {
		return fmt.Errorf("%w: candidate persistence failed before Start", ErrCandidateNotPromotable)
	}
	gen.phase = streamPhaseActive
	// Promote observed remote terminal into commit-pending. The output loop is
	// the sole terminal commit owner after signalStartResolved(true).
	if gen.terminalObs {
		gen.terminalPend = true
	}
	return nil
}

func getGeneration(jobId string) *streamGeneration {
	gen, ok := activeJobStreams.GetEx(jobId)
	if !ok {
		return nil
	}
	return gen
}

// beginGenerationDrain stops ingress on the generation (if pointer still current).
func beginGenerationDrain(jobId string, gen *streamGeneration) error {
	if gen == nil {
		return fmt.Errorf("nil generation")
	}
	if getGeneration(jobId) != gen {
		return fmt.Errorf("generation %s not current", gen.id)
	}
	gen.mu.Lock()
	if gen.phase == streamPhaseCandidate || gen.phase == streamPhaseActive {
		gen.phase = streamPhaseQuiescing
	}
	gen.mu.Unlock()
	if gen.reader != nil {
		gen.reader.BeginDrain()
	}
	return nil
}

// waitGenerationDone joins the output owner without interpreting its persistence
// result. DeleteJob uses this after cancellation because retained bytes are being
// explicitly discarded with the job.
func waitGenerationDone(ctx context.Context, gen *streamGeneration) error {
	if gen == nil || gen.done == nil {
		return nil
	}
	select {
	case <-gen.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// waitGenerationPersisted waits for the output loop and requires durable
// primary, terminal state, and mirror repair to be clean.
func waitGenerationPersisted(ctx context.Context, jobId string, gen *streamGeneration) error {
	if gen == nil {
		return nil
	}
	if gen.done == nil {
		return nil
	}
	select {
	case <-gen.done:
		if err := gen.Result(); err != nil {
			return err
		}
		return waitGenerationMirrorClean(ctx, jobId, gen)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// removeGenerationIf deletes the map entry only when the stored pointer is gen
// and durable primary + terminal + mirror outbox are clean. Stale removers
// cannot drop a replacement.
func removeGenerationIf(jobId string, gen *streamGeneration) bool {
	if gen == nil {
		return false
	}
	gen.mu.Lock()
	if len(gen.pending) > 0 || gen.terminalPend || len(gen.mirrorPending) > 0 {
		gen.mu.Unlock()
		return false
	}
	gen.mu.Unlock()
	removed := activeJobStreams.DeleteIf(jobId, func(cur *streamGeneration, exists bool) bool {
		return exists && cur == gen
	})
	if removed && gen.cancel != nil {
		gen.cancel()
	}
	return removed
}

// removeGenerationIfCurrent is the legacy stream-id path; prefers pointer when
// the map entry matches the id.
func removeGenerationIfCurrent(jobId string, streamId string) {
	cur := getGeneration(jobId)
	if cur == nil || cur.id != streamId {
		return
	}
	removeGenerationIf(jobId, cur)
}

// quiesceActiveJobStream gracefully drains the current generation:
//
//	ACTIVE/CANDIDATE -> QUIESCING (stop ingress/ACK) -> DRAINING (append buffer)
//	-> PERSISTED (done closed AND resultErr==nil) -> caller may SNAPSHOT WaveFS.
//
// On timeout or persistence failure the generation is KEPT and an error is returned;
// the caller must not snapshot WaveFS or start a new generation.
// If done already sealed with retained pending, redrive persistence once.
func quiesceActiveJobStream(jobId string, timeout time.Duration) error {
	old := getGeneration(jobId)
	if old == nil {
		return nil
	}
	quiesceCtx, quiesceCancel := context.WithTimeout(context.Background(), timeout)
	defer quiesceCancel()
	buffered := 0
	if old.reader != nil {
		buffered = old.reader.BufferedLen()
	}
	log.Printf("[job:%s] old stream quiescing (graceful drain): streamId=%s buffered=%d pending=%d",
		jobId, old.id, buffered, old.PendingLen())
	_ = beginGenerationDrain(jobId, old)
	if old.done == nil {
		removeGenerationIf(jobId, old)
		return nil
	}
	// If loop already finished with retained work, redrive before treating as final.
	select {
	case <-old.done:
		if rederr := redriveGenerationPersistence(quiesceCtx, jobId, old); rederr != nil {
			log.Printf("[job:%s] old stream redrive after sealed done failed: streamId=%s err=%v", jobId, old.id, rederr)
			return fmt.Errorf("old stream redrive failed: %w", rederr)
		}
		err := old.Result()
		if err != nil && (old.PendingLen() > 0 || genHasTerminalPend(old) || genHasMirrorPending(old)) {
			return fmt.Errorf("%w: %v", ErrStreamPersistence, err)
		}
		// If redrive cleared pending/terminal, allow remove even if old resultErr was set.
		if old.PendingLen() == 0 && !genHasTerminalPend(old) && !genHasMirrorPending(old) {
			log.Printf("[job:%s] old stream quiesced after redrive: streamId=%s", jobId, old.id)
			// Clear sealed error so remove succeeds path; force pointer delete if clean.
			old.mu.Lock()
			if len(old.pending) == 0 && !old.terminalPend && len(old.mirrorPending) == 0 {
				old.resultErr = nil
			}
			old.mu.Unlock()
			removeGenerationIf(jobId, old)
			return nil
		}
		if err != nil {
			return fmt.Errorf("%w: %v", ErrStreamPersistence, err)
		}
		removeGenerationIf(jobId, old)
		return nil
	default:
	}
	select {
	case <-old.done:
		// Final redrive opportunity for races where finish raced with last append.
		if rederr := redriveGenerationPersistence(quiesceCtx, jobId, old); rederr != nil {
			log.Printf("[job:%s] old stream quiesce redrive failed: streamId=%s err=%v", jobId, old.id, rederr)
			return fmt.Errorf("old stream quiesce redrive failed: %w", rederr)
		}
		err := old.Result()
		if err != nil && (old.PendingLen() > 0 || genHasTerminalPend(old)) {
			log.Printf("[job:%s] old stream quiesce persistence failed: streamId=%s err=%v", jobId, old.id, err)
			return fmt.Errorf("%w: %v", ErrStreamPersistence, err)
		}
		if old.PendingLen() == 0 && !genHasTerminalPend(old) && !genHasMirrorPending(old) {
			old.mu.Lock()
			old.resultErr = nil
			old.mu.Unlock()
		} else if err != nil {
			return fmt.Errorf("%w: %v", ErrStreamPersistence, err)
		}
		log.Printf("[job:%s] old stream quiesced: streamId=%s", jobId, old.id)
		removeGenerationIf(jobId, old)
		return nil
	case <-quiesceCtx.Done():
		log.Printf("[job:%s] old stream quiesce timeout (keeping generation): streamId=%s", jobId, old.id)
		return fmt.Errorf("%w: streamId=%s", ErrStreamQuiesceTimeout, old.id)
	}
}

func genHasTerminalPend(gen *streamGeneration) bool {
	if gen == nil {
		return false
	}
	gen.mu.Lock()
	defer gen.mu.Unlock()
	return gen.terminalPend
}

func genHasMirrorPending(gen *streamGeneration) bool {
	if gen == nil {
		return false
	}
	gen.mu.Lock()
	defer gen.mu.Unlock()
	return len(gen.mirrorPending) > 0
}

// installActiveJobStream registers a generation already in phaseActive.
// Used by StartJob after remote start succeeds (no separate StartStream RPC).
// Uses SetUnless — never overwrites a concurrent recovery candidate.
func installActiveJobStream(jobId string, streamId string, reader *streamclient.Reader) (*streamGeneration, error) {
	if isJobDeletingOrDeleted(jobId) {
		return nil, fmt.Errorf("%w: job is deleting", ErrJobDeleted)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ajs := &streamGeneration{
		id:        streamId,
		reader:    reader,
		ctx:       ctx,
		cancel:    cancel,
		phase:     streamPhaseActive,
		done:      make(chan struct{}),
		startWait: make(chan struct{}),
		startDone: true,
		startOK:   true,
		outcome:   loopOutcomeRunning,
	}
	// Close startWait immediately (already resolved).
	close(ajs.startWait)
	if !activeJobStreams.SetUnless(jobId, ajs) {
		cancel()
		cur := getGeneration(jobId)
		curId := ""
		if cur != nil {
			curId = cur.id
		}
		return nil, fmt.Errorf("%w: generation %s already present for job %s", ErrGenerationExists, curId, jobId)
	}
	if isJobDeletingOrDeleted(jobId) {
		activeJobStreams.DeleteIf(jobId, func(cur *streamGeneration, exists bool) bool {
			return exists && cur == ajs
		})
		cancel()
		return nil, fmt.Errorf("%w: job is deleting", ErrJobDeleted)
	}
	return ajs, nil
}

// clearActiveJobStreamIf deletes the entry only if it is still this stream id
// AND the same map pointer still matches (via removeGenerationIfCurrent).
func clearActiveJobStreamIf(jobId string, streamId string) {
	removeGenerationIfCurrent(jobId, streamId)
}

// isWriterTerminalStreamError reports whether err is a durable-stream writer
// terminal (PTY error packet), as opposed to local close or transport loss.
func isWriterTerminalStreamError(err error) bool {
	if err == nil {
		return false
	}
	// streamclient.Reader wraps writer Error packets as "stream error: …"
	return strings.HasPrefix(err.Error(), "stream error:")
}

// markJobStreamUnhealthy clears Connected status when we have no live reader
// so later ReconnectJob / recovery retries are not skipped as "already connected".
func markJobStreamUnhealthy(jobId string) {
	SetJobConnStatus(jobId, JobConnStatus_Disconnected)
	// Always publish so UI cannot remain stale-connected after a silent failure.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	job, err := wstore.DBGet[*waveobj.Job](ctx, jobId)
	if err != nil || job == nil {
		return
	}
	sendBlockJobStatusEventByJob(ctx, job)
}

func handleConnChangeEvent(event *wps.WaveEvent) {
	var connStatus wshrpc.ConnStatus
	err := utilfn.ReUnmarshal(&connStatus, event.Data)
	if err != nil {
		log.Printf("[connchange] error unmarshaling ConnStatus: %v", err)
		return
	}

	var connName string
	for _, scope := range event.Scopes {
		if strings.HasPrefix(scope, "connection:") {
			connName = strings.TrimPrefix(scope, "connection:")
			break
		}
	}
	if connName == "" {
		return
	}

	connStates.Lock()
	cs, exists := connStates.m[connName]
	if !exists {
		cs = &connState{actual: false, processed: false, reconciling: false}
		connStates.m[connName] = cs
	}
	cs.actual = connStatus.Connected
	connStates.Unlock()

	select {
	case connStates.reconcileCh <- struct{}{}:
	default:
	}
}

func handleBlockCloseEvent(event *wps.WaveEvent) {
	ctx, cancelFn := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelFn()
	blockId, ok := event.Data.(string)
	if !ok {
		log.Printf("[blockclose] invalid event data type")
		return
	}

	jobIds, err := wstore.WithTxRtn(ctx, func(tx *wstore.TxWrap) ([]string, error) {
		query := `SELECT oid FROM db_job WHERE json_extract(data, '$.attachedblockid') = ?`
		jobIds := tx.SelectStrings(query, blockId)
		return jobIds, nil
	})
	if err != nil {
		log.Printf("[block:%s] error looking up jobids: %v", blockId, err)
		return
	}
	if len(jobIds) == 0 {
		return
	}

	for _, jobId := range jobIds {
		TerminateAndDetachJob(ctx, jobId)
	}
}

func onConnectionUp(connName string) {
	log.Printf("[conn:%s] connection became connected, submitting per-job recovery", connName)
	ctx, cancelFn := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelFn()

	allJobs, err := wstore.DBGetAllObjsByType[*waveobj.Job](ctx, waveobj.OType_Job)
	if err != nil {
		log.Printf("[conn:%s] failed to get jobs for reconnection: %v", connName, err)
		return
	}

	var jobsToReconnect []*waveobj.Job
	for _, job := range allJobs {
		if job.Connection == connName && isJobManagerRunning(job) {
			jobsToReconnect = append(jobsToReconnect, job)
		}
	}

	log.Printf("[conn:%s] found %d jobs to reconnect via recovery coordinator", connName, len(jobsToReconnect))

	// Each job gets its own recovery owner and independent attempt budget —
	// never share one short context across the whole batch.
	for _, job := range jobsToReconnect {
		wakeStreamRecovery(job.OID)
		requestStreamRecovery(job.OID, connName, "connection-up", true)
	}
}

func onConnectionDown(connName string) {
	log.Printf("[conn:%s] connection became disconnected", connName)
}

func GetJobConnStatus(jobId string) string {
	jobControllerLock.Lock()
	defer jobControllerLock.Unlock()
	status, exists := jobConnStates[jobId]
	if !exists {
		return JobConnStatus_Disconnected
	}
	return status
}

func SetJobConnStatus(jobId string, status string) {
	jobControllerLock.Lock()
	defer jobControllerLock.Unlock()
	if status == JobConnStatus_Disconnected {
		delete(jobConnStates, jobId)
	} else {
		jobConnStates[jobId] = status
	}
}

func GetConnectedJobIds() []string {
	jobControllerLock.Lock()
	defer jobControllerLock.Unlock()
	var connectedJobIds []string
	for jobId, status := range jobConnStates {
		if status == JobConnStatus_Connected {
			connectedJobIds = append(connectedJobIds, jobId)
		}
	}
	return connectedJobIds
}

func GetNumJobsRunning() int {
	ctx, cancelFn := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelFn()
	allJobs, err := wstore.DBGetAllObjsByType[*waveobj.Job](ctx, waveobj.OType_Job)
	if err != nil {
		return 0
	}
	count := 0
	for _, job := range allJobs {
		if job.JobManagerStatus == JobManagerStatus_Running {
			count++
		}
	}
	return count
}

func GetNumJobsConnected() int {
	jobControllerLock.Lock()
	defer jobControllerLock.Unlock()
	count := 0
	for _, status := range jobConnStates {
		if status == JobConnStatus_Connected {
			count++
		}
	}
	return count
}

func CheckJobConnected(ctx context.Context, jobId string) (*waveobj.Job, error) {
	job, err := wstore.DBMustGet[*waveobj.Job](ctx, jobId)
	if err != nil {
		return nil, fmt.Errorf("failed to get job: %w", err)
	}

	isConnected, err := conncontroller.IsConnected(job.Connection)
	if err != nil {
		return nil, fmt.Errorf("error checking connection status: %w", err)
	}
	if !isConnected {
		return nil, fmt.Errorf("connection %q is not connected", job.Connection)
	}
	if !isJobRouteUp(jobId) {
		return nil, fmt.Errorf("job route is not connected")
	}

	jobConnStatus := GetJobConnStatus(jobId)
	if jobConnStatus != JobConnStatus_Connected {
		return nil, fmt.Errorf("job is not connected (status: %s)", jobConnStatus)
	}

	// Connected without a promoted active reader is half-open (candidate-only
	// also fails). StreamDone is the only case where no reader is healthy.
	if !job.StreamDone && !isHealthyActiveGeneration(jobId) {
		return nil, fmt.Errorf("job is connected but has no active stream reader")
	}

	return job, nil
}

type StartJobParams struct {
	ConnName string
	JobKind  string
	Cmd      string
	Args     []string
	Env      map[string]string
	TermSize *waveobj.TermSize
	BlockId  string
}

func StartJob(ctx context.Context, params StartJobParams) (string, error) {
	if params.ConnName == "" {
		return "", fmt.Errorf("connection name is required")
	}
	if params.JobKind != JobKind_Shell && params.JobKind != JobKind_Task {
		return "", fmt.Errorf("jobkind must be %q or %q", JobKind_Shell, JobKind_Task)
	}
	if params.Cmd == "" {
		return "", fmt.Errorf("command is required")
	}
	if params.TermSize == nil {
		params.TermSize = &waveobj.TermSize{Rows: 24, Cols: 80}
	}

	isConnected, err := conncontroller.IsConnected(params.ConnName)
	if err != nil {
		return "", fmt.Errorf("error checking connection status: %w", err)
	}
	if !isConnected {
		return "", fmt.Errorf("connection %q is not connected", params.ConnName)
	}

	jobId := uuid.New().String()
	jobAuthToken, err := utilfn.RandomHexString(32)
	if err != nil {
		return "", fmt.Errorf("failed to generate job auth token: %w", err)
	}

	jobAccessClaims := &wavejwt.WaveJwtClaims{
		MainServer: true,
		JobId:      jobId,
	}
	jobAccessToken, err := wavejwt.Sign(jobAccessClaims)
	if err != nil {
		return "", fmt.Errorf("failed to generate job access token: %w", err)
	}

	job := &waveobj.Job{
		OID:              jobId,
		Connection:       params.ConnName,
		JobKind:          params.JobKind,
		Cmd:              params.Cmd,
		CmdArgs:          params.Args,
		CmdEnv:           params.Env,
		CmdTermSize:      *params.TermSize,
		JobAuthToken:     jobAuthToken,
		JobManagerStatus: JobManagerStatus_Init,
		AttachedBlockId:  params.BlockId,
		WaveVersion:      wavebase.WaveVersion,
		Meta:             make(waveobj.MetaMapType),
	}

	err = wstore.DBInsert(ctx, job)
	if err != nil {
		return "", fmt.Errorf("failed to create job in database: %w", err)
	}
	// Mark initializing before remote start so early route-up only records routeUp
	// and does not start recovery that would race installActiveJobStream.
	beginJobInitializing(jobId)
	defer clearJobInitializing(jobId)
	if params.BlockId != "" {
		// AttachJobToBlock will send status
		err = AttachJobToBlock(ctx, jobId, params.BlockId)
		if err != nil {
			return "", fmt.Errorf("failed to attach job to block: %w", err)
		}
	}
	bareRpc := wshclient.GetBareRpcClient()
	broker := bareRpc.StreamBroker
	readerRouteId := wshclient.GetBareRpcClientRouteId()
	writerRouteId := wshutil.MakeJobRouteId(jobId)
	reader, streamMeta := broker.CreateStreamReader(readerRouteId, writerRouteId, DefaultStreamRwnd)

	fileOpts := wshrpc.FileOpts{
		MaxSize:  10 * 1024 * 1024,
		Circular: true,
	}
	err = filestore.WFS.MakeFile(ctx, jobId, JobOutputFileName, wshrpc.FileMeta{}, fileOpts)
	if err != nil {
		reader.Close()
		return "", fmt.Errorf("failed to create WaveFS file: %w", err)
	}

	clientId := wstore.GetClientId()
	publicKey := wavejwt.GetPublicKey()
	publicKeyBase64 := base64.StdEncoding.EncodeToString(publicKey)
	jobEnv := envutil.CopyAndAddToEnvMap(params.Env, "WAVETERM_JOBID", jobId)
	startJobData := wshrpc.CommandRemoteStartJobData{
		Cmd:                params.Cmd,
		Args:               params.Args,
		Env:                jobEnv,
		TermSize:           *params.TermSize,
		StreamMeta:         streamMeta,
		JobAuthToken:       jobAuthToken,
		JobId:              jobId,
		MainServerJwtToken: jobAccessToken,
		ClientId:           clientId,
		PublicKeyBase64:    publicKeyBase64,
	}

	rpcOpts := &wshrpc.RpcOpts{
		Route:   wshutil.MakeConnectionRouteId(params.ConnName),
		Timeout: 30000,
	}

	writeSessionSeparatorToTerminal(params.BlockId, params.TermSize.Cols)

	log.Printf("[job:%s] sending RemoteStartJobCommand to connection %s, cmd=%q, args=%v", jobId, params.ConnName, params.Cmd, params.Args)
	log.Printf("[job:%s] env=%v", jobId, params.Env)
	rtnData, err := wshclient.RemoteStartJobCommand(bareRpc, startJobData, rpcOpts)
	if err != nil {
		reader.Close()
		clearJobInitializing(jobId)
		log.Printf("[job:%s] RemoteStartJobCommand failed: %v", jobId, err)
		errMsg := fmt.Sprintf("failed to start job: %v", err)
		var updatedJob *waveobj.Job
		wstore.DBUpdateFn(ctx, jobId, func(job *waveobj.Job) {
			job.JobManagerStatus = JobManagerStatus_Done
			job.JobManagerDoneReason = JobDoneReason_StartupError
			job.JobManagerStartupError = errMsg
			updatedJob = job
		})
		sendBlockJobStatusEventByJob(ctx, updatedJob)
		telemetry.GoRecordTEventWrap(&telemetrydata.TEvent{
			Event: "job:done",
			Props: telemetrydata.TEventProps{
				JobDoneReason: JobDoneReason_StartupError,
				JobKind:       params.JobKind,
			},
		})
		return "", fmt.Errorf("failed to start remote job: %w", err)
	}

	log.Printf("[job:%s] RemoteStartJobCommand succeeded, cmdpid=%d cmdstartts=%d jobmanagerpid=%d jobmanagerstartts=%d", jobId, rtnData.CmdPid, rtnData.CmdStartTs, rtnData.JobManagerPid, rtnData.JobManagerStartTs)
	var updatedJob *waveobj.Job
	err = wstore.DBUpdateFn(ctx, jobId, func(job *waveobj.Job) {
		job.CmdPid = rtnData.CmdPid
		job.CmdStartTs = rtnData.CmdStartTs
		job.JobManagerPid = rtnData.JobManagerPid
		job.JobManagerStartTs = rtnData.JobManagerStartTs
		job.JobManagerStatus = JobManagerStatus_Running
		updatedJob = job
	})
	if err != nil {
		log.Printf("[job:%s] warning: failed to update job status to running: %v", jobId, err)
	} else {
		log.Printf("[job:%s] job status updated to running", jobId)
		sendBlockJobStatusEventByJob(ctx, updatedJob)
	}

	telemetry.GoRecordTEventWrap(&telemetrydata.TEvent{
		Event: "job:start",
		Props: telemetrydata.TEventProps{
			JobKind: params.JobKind,
		},
	})

	// Install active stream only after remote start succeeded (CAS — no overwrite).
	// First-start lifecycle: Running + Connected published only after install under
	// lifecycle checks so route-down/delete cannot leave a false Connected.
	// Route may already be up (remote registers before RemoteStart returns).
	routeId := wshutil.MakeJobRouteId(jobId)
	routeEpoch := getJobRouteEpoch(jobId)
	routeCtx, routeCancel := context.WithTimeout(ctx, 50*time.Millisecond)
	if wshutil.DefaultRouter.WaitForRegister(routeCtx, routeId) == nil {
		_ = confirmJobRouteUp(jobId, routeEpoch)
	}
	routeCancel()

	ajs, ierr := installActiveJobStream(jobId, streamMeta.Id, reader)
	if ierr != nil {
		reader.Close()
		clearJobInitializing(jobId)
		return "", fmt.Errorf("failed to install active stream: %w", ierr)
	}
	go func() {
		defer func() {
			panichandler.PanicHandler("jobcontroller:runOutputLoop", recover())
		}()
		runOutputLoop(ajs.ctx, jobId, ajs)
	}()

	// Complete the initialization/route-up handoff in one lifecycle transition.
	// route-down/DeleteJob use the same lock and therefore always win by order.
	if finishJobInitialization(jobId) {
		// Re-check: route-down/delete may have raced after the checks above.
		if isJobDeletingOrDeleted(jobId) || !isJobRouteUp(jobId) {
			SetJobConnStatus(jobId, JobConnStatus_Disconnected)
			log.Printf("[job:%s] first start: Connected reverted after route-down/delete race", jobId)
		} else {
			sendBlockJobStatusEventByJob(ctx, updatedJob)
			log.Printf("[job:%s] first start: active stream installed and Connected published", jobId)
		}
	} else {
		clearJobInitializing(jobId)
		log.Printf("[job:%s] first start: stream installed but Connected deferred (routeUp=%v)", jobId, isJobRouteUp(jobId))
		// If stream is healthy and route comes up later, recovery/route-up will
		// reconnect or we publish Connected when route-up sees healthy active.
	}

	return jobId, nil
}

func doWFSAppend(ctx context.Context, oref waveobj.ORef, fileName string, data []byte) error {
	offset, err := filestore.WFS.AppendDataGetOffset(ctx, oref.OID, fileName, data)
	if err != nil {
		return err
	}
	wps.Broker.Publish(wps.WaveEvent{
		Event: wps.Event_BlockFile,
		Scopes: []string{
			oref.String(),
		},
		Data: &wps.WSFileEventData{
			ZoneId:   oref.OID,
			FileName: fileName,
			FileOp:   wps.FileOp_Append,
			Data64:   base64.StdEncoding.EncodeToString(data),
			Offset:   offset,
		},
	})
	return nil
}

// appendDurableJobOutput commits bytes to the job WaveFS file that owns resume seq.
func appendDurableJobOutput(ctx context.Context, jobId string, fileName string, data []byte) error {
	err := doWFSAppend(ctx, waveobj.MakeORef(waveobj.OType_Job, jobId), fileName, data)
	if err != nil {
		return fmt.Errorf("error appending to job file: %w", err)
	}
	return nil
}

// appendBlockOutputMirror mirrors to the attached block. Failures must NOT
// re-append the durable primary file.
func appendBlockOutputMirror(ctx context.Context, jobId string, fileName string, data []byte) error {
	job, err := wstore.DBGet[*waveobj.Job](ctx, jobId)
	if err != nil {
		return fmt.Errorf("error getting job for block mirror: %w", err)
	}
	if job == nil || job.AttachedBlockId == "" {
		return nil
	}
	err = doWFSAppend(ctx, waveobj.MakeORef(waveobj.OType_Block, job.AttachedBlockId), fileName, data)
	if err != nil {
		return fmt.Errorf("error appending to block file: %w", err)
	}
	return nil
}

// handleAppendJobFile keeps the historical combined API: primary first, then mirror.
// Primary success is durable; mirror failure is returned but callers that already
// committed primary must not re-run primary.
func handleAppendJobFile(ctx context.Context, jobId string, fileName string, data []byte) error {
	if err := appendDurableJobOutputFn(ctx, jobId, fileName, data); err != nil {
		return err
	}
	if err := appendBlockMirrorFn(ctx, jobId, fileName, data); err != nil {
		// Primary already committed — surface mirror error without undoing primary.
		return err
	}
	return nil
}

const mirrorRepairChunkSize = 64 * 1024

func ensureMirrorChannelsLocked(gen *streamGeneration) {
	if gen.mirrorWake == nil {
		gen.mirrorWake = make(chan struct{}, 1)
	}
	if gen.mirrorSpace == nil {
		gen.mirrorSpace = make(chan struct{}, 1)
	}
}

func signalMirrorChannel(ch chan struct{}) {
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

func startMirrorRepairWorker(jobId string, gen *streamGeneration) {
	if gen == nil {
		return
	}
	gen.mu.Lock()
	ensureMirrorChannelsLocked(gen)
	if gen.mirrorRunning {
		wake := gen.mirrorWake
		gen.mu.Unlock()
		signalMirrorChannel(wake)
		return
	}
	if len(gen.mirrorPending) == 0 || gen.mirrorInvErr != nil {
		gen.mu.Unlock()
		return
	}
	ctx := gen.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	gen.mirrorRunning = true
	gen.mirrorDone = make(chan struct{})
	done := gen.mirrorDone
	appendMirror := appendBlockMirrorFn
	gen.mu.Unlock()
	go runMirrorRepairWorker(ctx, jobId, gen, done, appendMirror)
}

func runMirrorRepairWorker(
	ctx context.Context,
	jobId string,
	gen *streamGeneration,
	done chan struct{},
	appendMirror func(context.Context, string, string, []byte) error,
) {
	defer func() {
		gen.mu.Lock()
		gen.mirrorRunning = false
		space := gen.mirrorSpace
		gen.mu.Unlock()
		close(done)
		signalMirrorChannel(space)
	}()

	attempt := 0
	for {
		gen.mu.Lock()
		ensureMirrorChannelsLocked(gen)
		if gen.mirrorInvErr != nil {
			gen.mu.Unlock()
			return
		}
		if len(gen.mirrorPending) == 0 {
			wake := gen.mirrorWake
			gen.mu.Unlock()
			select {
			case <-ctx.Done():
				return
			case <-wake:
				continue
			}
		}
		drainLen := len(gen.mirrorPending)
		if drainLen > mirrorRepairChunkSize {
			drainLen = mirrorRepairChunkSize
		}
		chunk := append([]byte(nil), gen.mirrorPending[:drainLen]...)
		gen.mu.Unlock()

		err := appendMirror(ctx, jobId, JobOutputFileName, chunk)
		if err == nil {
			gen.mu.Lock()
			if len(gen.mirrorPending) < len(chunk) || !bytes.Equal(gen.mirrorPending[:len(chunk)], chunk) {
				gen.mirrorInvErr = fmt.Errorf("%w: mirror prefix changed during repair", ErrStreamInvariant)
			} else {
				gen.mirrorPending = gen.mirrorPending[len(chunk):]
				gen.mirrorLastErr = nil
			}
			space := gen.mirrorSpace
			invErr := gen.mirrorInvErr
			mirrorClean := len(gen.mirrorPending) == 0
			gen.mu.Unlock()
			signalMirrorChannel(space)
			if invErr != nil {
				return
			}
			if mirrorClean {
				removeFinishedGenerationAfterMirrorRepair(jobId, gen)
			}
			attempt = 0
			continue
		}

		attempt++
		gen.mu.Lock()
		gen.mirrorLastErr = err
		gen.mu.Unlock()
		idx := attempt - 1
		if idx >= len(streamAppendRetryDelays) {
			idx = len(streamAppendRetryDelays) - 1
		}
		delay := streamAppendRetryDelays[idx]
		log.Printf("[job:%s] block mirror repair failed (attempt=%d): %v", jobId, attempt, err)
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		}
	}
}

// removeFinishedGenerationAfterMirrorRepair closes the cleanup handoff between
// the output owner and mirror owner. The output owner removes a clean generation;
// if it finished while mirror bytes were still pending, the mirror owner removes
// it after the last chunk commits. A failed output result remains installed for
// recovery/redrive, matching runOutputLoop's normal cleanup condition.
func removeFinishedGenerationAfterMirrorRepair(jobId string, gen *streamGeneration) {
	select {
	case <-gen.done:
	default:
		return
	}

	gen.mu.Lock()
	removable := gen.resultErr == nil &&
		(gen.phase != streamPhaseCandidate || (gen.startDone && !gen.startOK))
	gen.mu.Unlock()
	if removable {
		removeGenerationIf(jobId, gen)
	}
}

func waitForMirrorCapacity(ctx context.Context, jobId string, gen *streamGeneration, need int) error {
	if need < 0 || need > StreamMirrorOutboxMax {
		return fmt.Errorf("%w: mirror reservation %d exceeds cap %d", ErrStreamInvariant, need, StreamMirrorOutboxMax)
	}
	for {
		gen.mu.Lock()
		ensureMirrorChannelsLocked(gen)
		if gen.mirrorInvErr != nil {
			err := gen.mirrorInvErr
			gen.mu.Unlock()
			return err
		}
		if len(gen.mirrorPending)+need <= StreamMirrorOutboxMax {
			gen.mu.Unlock()
			return nil
		}
		pending := len(gen.mirrorPending)
		lastErr := gen.mirrorLastErr
		space := gen.mirrorSpace
		var genDone <-chan struct{}
		if gen.ctx != nil {
			genDone = gen.ctx.Done()
		}
		gen.mu.Unlock()
		startMirrorRepairWorker(jobId, gen)
		select {
		case <-space:
			continue
		case <-ctx.Done():
			return fmt.Errorf("%w: outbox=%d need=%d last-error=%v: %v", ErrStreamMirrorPending, pending, need, lastErr, ctx.Err())
		case <-genDone:
			return fmt.Errorf("%w: generation canceled with outbox=%d", ErrStreamMirrorPending, pending)
		}
	}
}

func waitGenerationMirrorClean(ctx context.Context, jobId string, gen *streamGeneration) error {
	if gen == nil {
		return nil
	}
	for {
		gen.mu.Lock()
		ensureMirrorChannelsLocked(gen)
		if gen.mirrorInvErr != nil {
			err := gen.mirrorInvErr
			gen.mu.Unlock()
			return err
		}
		if len(gen.mirrorPending) == 0 {
			gen.mu.Unlock()
			return nil
		}
		pending := len(gen.mirrorPending)
		lastErr := gen.mirrorLastErr
		space := gen.mirrorSpace
		var genDone <-chan struct{}
		if gen.ctx != nil {
			genDone = gen.ctx.Done()
		}
		gen.mu.Unlock()
		startMirrorRepairWorker(jobId, gen)
		select {
		case <-space:
			continue
		case <-ctx.Done():
			return fmt.Errorf("%w: outbox=%d last-error=%v: %v", ErrStreamMirrorPending, pending, lastErr, ctx.Err())
		case <-genDone:
			return fmt.Errorf("%w: generation canceled with outbox=%d", ErrStreamMirrorPending, pending)
		}
	}
}

func waitMirrorRepairDone(ctx context.Context, gen *streamGeneration) error {
	if gen == nil {
		return nil
	}
	for {
		gen.mu.Lock()
		running := gen.mirrorRunning
		done := gen.mirrorDone
		gen.mu.Unlock()
		if !running || done == nil {
			return nil
		}
		select {
		case <-done:
			continue
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func waitMirrorRepairStopped(gen *streamGeneration, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return waitMirrorRepairDone(ctx, gen) == nil
}

// persistGenerationPending commits gen.pending to durable WaveFS with retries.
// On primary success, pending is cleared only when bytes.Equal matches the
// committed prefix. Mirror failures go to mirrorPending (never re-run primary).
//
// After StreamPersistMaxAttempts the function switches to slow cancellable
// backoff and keeps trying — it does NOT seal permanent failure. Only cancel
// (caller ctx / gen.ctx) or invariant errors return failure with pending intact.
// Serialized by gen.persistGate so recovery redrive and the output loop cannot
// double-append primary; waiting for ownership remains caller-cancellable.
func persistGenerationPending(ctx context.Context, jobId string, gen *streamGeneration) error {
	return persistGenerationPendingOpts(ctx, jobId, gen, true)
}

// persistGenerationPendingOpts: if allowSlow is false, return after the fast budget
// so redrive/quiesce cannot hang forever on permanent WaveFS failure (caller retries later).
func persistGenerationPendingOpts(ctx context.Context, jobId string, gen *streamGeneration, allowSlow bool) error {
	if gen == nil {
		return nil
	}
	runCtx, cancel := mergePersistenceContext(ctx, gen.ctx)
	defer cancel()
	gate := generationPersistenceGate(gen)
	select {
	case <-gate:
		defer func() { gate <- struct{}{} }()
	case <-runCtx.Done():
		return fmt.Errorf("%w: waiting for persistence owner: %v", ErrStreamPersistence, runCtx.Err())
	}
	return persistGenerationPendingLocked(runCtx, jobId, gen, allowSlow)
}

func generationPersistenceGate(gen *streamGeneration) chan struct{} {
	gen.persistGateMu.Lock()
	defer gen.persistGateMu.Unlock()
	if gen.persistGate == nil {
		gen.persistGate = make(chan struct{}, 1)
		gen.persistGate <- struct{}{}
	}
	return gen.persistGate
}

func mergePersistenceContext(caller context.Context, generation context.Context) (context.Context, context.CancelFunc) {
	if caller == nil {
		caller = context.Background()
	}
	runCtx, cancel := context.WithCancel(caller)
	if generation == nil {
		return runCtx, cancel
	}
	if generation.Err() != nil {
		cancel()
		return runCtx, cancel
	}
	stopGenerationCancel := context.AfterFunc(generation, cancel)
	return runCtx, func() {
		stopGenerationCancel()
		cancel()
	}
}

func persistGenerationPendingLocked(ctx context.Context, jobId string, gen *streamGeneration, allowSlow bool) error {
	attempt := 0
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%w: %v", ErrStreamPersistence, err)
		}

		gen.mu.Lock()
		if len(gen.pending) == 0 {
			hasMirrorPending := len(gen.mirrorPending) > 0
			gen.mu.Unlock()
			if hasMirrorPending {
				startMirrorRepairWorker(jobId, gen)
			}
			restoreGenerationActiveIfDurable(jobId, gen)
			return nil
		}
		chunk := make([]byte, len(gen.pending))
		copy(chunk, gen.pending)
		gen.mu.Unlock()

		// Reserve bounded mirror capacity before committing primary. When the
		// outbox is full this blocks the output reader, shrinking RWnd naturally.
		if err := waitForMirrorCapacity(ctx, jobId, gen, len(chunk)); err != nil {
			return err
		}
		err := appendDurableJobOutputFn(ctx, jobId, JobOutputFileName, chunk)
		if err == nil {
			gen.mu.Lock()
			if len(gen.pending) >= len(chunk) && bytes.Equal(gen.pending[:len(chunk)], chunk) {
				gen.pending = gen.pending[len(chunk):]
			} else {
				// Invariant: single writer must not diverge. Keep pending intact.
				inv := fmt.Errorf("%w: pending prefix mismatch after durable commit (chunk=%d pending=%d)",
					ErrStreamInvariant, len(chunk), len(gen.pending))
				gen.mu.Unlock()
				return inv
			}
			// Queue mirror separately — never re-append primary. Capacity was
			// reserved before the primary commit under the single persistence gate.
			if len(gen.mirrorPending)+len(chunk) > StreamMirrorOutboxMax {
				inv := fmt.Errorf("%w: mirror reservation lost after primary commit", ErrStreamInvariant)
				gen.mirrorInvErr = inv
				gen.mu.Unlock()
				return inv
			}
			gen.mirrorPending = append(gen.mirrorPending, chunk...)
			gen.mu.Unlock()
			startMirrorRepairWorker(jobId, gen)
			restoreGenerationActiveIfDurable(jobId, gen)
			// Reset fast budget after a success so transient blips don't stick in slow mode.
			attempt = 0
			continue
		}

		log.Printf("[job:%s] durable WaveFS append failed (attempt=%d pending=%d): %v",
			jobId, attempt+1, len(chunk), err)
		attempt++
		// Fast budget → slow cancellable backoff (output loop). Redrive uses fast-only
		// so a single recovery tick cannot hang for minutes.
		var delay time.Duration
		if attempt <= StreamPersistMaxAttempts {
			idx := attempt - 1
			if idx >= len(streamAppendRetryDelays) {
				idx = len(streamAppendRetryDelays) - 1
			}
			delay = streamAppendRetryDelays[idx]
		} else if !allowSlow {
			gen.mu.Lock()
			if gen.phase == streamPhaseActive {
				gen.phase = streamPhaseFailedPersisting
			}
			gen.mu.Unlock()
			markJobStreamUnhealthy(jobId)
			return fmt.Errorf("%w: exceeded %d append attempts: %v", ErrStreamPersistence, StreamPersistMaxAttempts, err)
		} else {
			slowIdx := attempt - StreamPersistMaxAttempts - 1
			if slowIdx >= len(streamPersistSlowDelays) {
				slowIdx = len(streamPersistSlowDelays) - 1
			}
			delay = streamPersistSlowDelays[slowIdx]
			// Surface degraded phase so health/UI are unhealthy while we keep retrying.
			gen.mu.Lock()
			if gen.phase == streamPhaseActive {
				gen.phase = streamPhaseFailedPersisting
			}
			gen.mu.Unlock()
			markJobStreamUnhealthy(jobId)
		}
		// Cancellable sleep via injectable hook (tests use no-op / short sleep).
		if err := cancellablePersistSleep(delay, ctx, nil); err != nil {
			return fmt.Errorf("%w: %v", ErrStreamPersistence, err)
		}
	}
}

func restoreGenerationActiveIfDurable(jobId string, gen *streamGeneration) bool {
	if gen == nil || getGeneration(jobId) != gen {
		return false
	}
	gen.mu.Lock()
	restored := gen.phase == streamPhaseFailedPersisting &&
		len(gen.pending) == 0 && !gen.terminalPend && !gen.loopExited
	if restored {
		gen.phase = streamPhaseActive
	}
	gen.mu.Unlock()
	if !restored || !isHealthyActiveGeneration(jobId) || !publishConnectedIfAllowed(jobId) {
		return restored
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	job, err := wstore.DBGet[*waveobj.Job](ctx, jobId)
	if err == nil && job != nil {
		sendBlockJobStatusEventByJob(ctx, job)
	}
	cancel()
	return true
}

// cancellablePersistSleep runs streamAppendRetrySleep while honoring cancel channels.
// Captures the sleep hook locally so tests can restore the global without racing
// a still-running sleep goroutine after cancel returns.
func cancellablePersistSleep(delay time.Duration, ctx context.Context, genDone <-chan struct{}) error {
	if delay <= 0 {
		return nil
	}
	sleepFn := streamAppendRetrySleep
	done := make(chan struct{})
	go func() {
		sleepFn(delay)
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-genDone:
		return context.Canceled
	}
}

// redriveGenerationPersistence re-attempts primary pending + terminal StreamDone
// for a generation that may already have sealed done. Used by recovery/quiesce
// so a temporary WaveFS outage is not permanent for the process lifetime.
// Uses fast-only persist budget; recovery owner will call again on later attempts.
func redriveGenerationPersistence(ctx context.Context, jobId string, gen *streamGeneration) error {
	if gen == nil {
		return nil
	}
	// Cap redrive so recovery attempts cannot hang forever on permanent WaveFS death.
	rctx, cancel := context.WithTimeout(ctx, OldStreamQuiesceTimeout)
	defer cancel()
	if gen.PendingLen() > 0 || genHasMirrorPending(gen) {
		if err := persistGenerationPendingOpts(rctx, jobId, gen, false); err != nil {
			return err
		}
	}
	if genHasMirrorPending(gen) {
		if err := waitGenerationMirrorClean(rctx, jobId, gen); err != nil {
			return err
		}
	}
	gen.mu.Lock()
	termPend := gen.terminalPend
	termEOF := gen.terminalEOF
	termErr := gen.terminalErr
	phase := gen.phase
	gen.mu.Unlock()
	if termPend && (phase == streamPhaseActive || phase == streamPhaseFailedPersisting || phase == streamPhaseQuiescing) {
		msg := ""
		if termErr != nil {
			msg = termErr.Error()
		} else if !termEOF {
			msg = ""
		}
		if err := reconcileGenerationTerminal(rctx, jobId, gen, msg); err != nil {
			return err
		}
	}
	return nil
}

func runOutputLoop(ctx context.Context, jobId string, ajs *streamGeneration) {
	streamId := ajs.id
	reader := ajs.reader
	// Prefer generation-scoped cancellable context.
	if ajs.ctx != nil {
		ctx = ajs.ctx
	}
	var loopErr error
	defer func() {
		// Final attempt to flush pending before finish (slow backoff allowed).
		if ajs.PendingLen() > 0 || genHasMirrorPending(ajs) {
			if perr := persistGenerationPending(ctx, jobId, ajs); perr != nil && loopErr == nil {
				loopErr = perr
			}
		}
		if reader != nil {
			ajs.mu.Lock()
			outcome := ajs.outcome
			ajs.mu.Unlock()
			if outcome == loopOutcomeDrained || outcome == loopOutcomeRemoteTerminal {
				if releaseErr := reader.ReleaseWithoutCancel(); releaseErr != nil {
					log.Printf("[job:%s] [stream:%s] local reader release failed; hard-cancelling: %v", jobId, streamId, releaseErr)
					_ = reader.Close()
				}
			} else {
				_ = reader.Close()
			}
		}
		ajs.mu.Lock()
		ajs.loopExited = true
		ajs.mu.Unlock()
		ajs.finish(loopErr)
		// Candidate generations must NOT self-remove before Start resolves —
		// a fast EOF would otherwise delete the map entry and break promote.
		ajs.mu.Lock()
		phase := ajs.phase
		startDone := ajs.startDone
		startOK := ajs.startOK
		pending := len(ajs.pending)
		termPend := ajs.terminalPend
		mirrorPend := len(ajs.mirrorPending)
		ajs.mu.Unlock()
		if phase != streamPhaseCandidate && loopErr == nil && pending == 0 && !termPend && mirrorPend == 0 {
			removeGenerationIf(jobId, ajs)
		} else if phase == streamPhaseCandidate && startDone && !startOK && loopErr == nil && pending == 0 && !termPend {
			// Start failed and primary drain completed — clear observed terminal
			// (not StreamDone commit-pending) and remove by pointer.
			ajs.mu.Lock()
			ajs.terminalObs = false
			ajs.terminalPend = false
			ajs.mu.Unlock()
			removeGenerationIf(jobId, ajs)
		}
		// Terminal commit failed: publish unhealthy so UI is not half-open Connected.
		if termPend || loopErr != nil {
			markJobStreamUnhealthy(jobId)
		}
		log.Printf("[job:%s] [stream:%s] output loop finished err=%v pending=%d phase=%d",
			jobId, streamId, loopErr, ajs.PendingLen(), phase)
	}()

	log.Printf("[job:%s] [stream:%s] output loop started", jobId, streamId)
	buf := make([]byte, 4096)
	for {
		if ajs.PendingLen() > 0 {
			if err := persistGenerationPending(ctx, jobId, ajs); err != nil {
				loopErr = err
				ajs.mu.Lock()
				ajs.outcome = loopOutcomePersistenceFailed
				ajs.phase = streamPhaseFailedPersisting
				ajs.mu.Unlock()
				log.Printf("[job:%s] [stream:%s] persist failed: %v", jobId, streamId, err)
				markJobStreamUnhealthy(jobId)
				return
			}
		}

		n, err := reader.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			ajs.mu.Lock()
			ajs.pending = append(ajs.pending, chunk...)
			ajs.mu.Unlock()
			if perr := persistGenerationPending(ctx, jobId, ajs); perr != nil {
				loopErr = perr
				ajs.mu.Lock()
				ajs.outcome = loopOutcomePersistenceFailed
				ajs.phase = streamPhaseFailedPersisting
				ajs.mu.Unlock()
				markJobStreamUnhealthy(jobId)
				return
			}
		}

		// Pointer identity: superseded if map no longer holds this gen.
		if getGeneration(jobId) != ajs {
			log.Printf("[job:%s] [stream:%s] stream superseded, exiting output loop (pending=%d)",
				jobId, streamId, ajs.PendingLen())
			ajs.mu.Lock()
			ajs.outcome = loopOutcomeSuperseded
			ajs.mu.Unlock()
			return
		}

		if err == io.EOF {
			if reader.IsDraining() {
				log.Printf("[job:%s] [stream:%s] graceful drain complete", jobId, streamId)
				ajs.mu.Lock()
				ajs.outcome = loopOutcomeDrained
				ajs.mu.Unlock()
				return
			}
			if getGeneration(jobId) != ajs {
				ajs.mu.Lock()
				ajs.outcome = loopOutcomeSuperseded
				ajs.mu.Unlock()
				return
			}
			ajs.mu.Lock()
			phase := ajs.phase
			ajs.terminalEOF = true
			ajs.terminalObs = true
			ajs.outcome = loopOutcomeRemoteTerminal
			// terminalPend only after promote (or already active).
			if phase == streamPhaseActive {
				ajs.terminalPend = true
			}
			ajs.mu.Unlock()
			if phase == streamPhaseCandidate {
				log.Printf("[job:%s] [stream:%s] EOF while candidate — waiting for Start resolution", jobId, streamId)
				// Do not finish/remove until Start path signals; block here so
				// defer does not race promote, but keep generation in map.
				_ = ajs.waitStartResolved(ctx)
				// After Start resolves: if promoted, commit terminal (sole owner here
				// when Start path does not also commit). Start fail: drop terminalPend.
				ajs.mu.Lock()
				phase = ajs.phase
				startOK := ajs.startOK
				if phase == streamPhaseActive && startOK {
					ajs.terminalPend = true
				}
				ajs.mu.Unlock()
				if phase == streamPhaseActive && startOK {
					if cerr := reconcileGenerationTerminal(ctx, jobId, ajs, ""); cerr != nil {
						loopErr = cerr
						ajs.mu.Lock()
						ajs.phase = streamPhaseFailedPersisting
						ajs.mu.Unlock()
						markJobStreamUnhealthy(jobId)
						return
					}
				} else {
					// Start failed — do not seal unpromoted terminal as irreversible Result.
					ajs.mu.Lock()
					ajs.terminalPend = false
					ajs.mu.Unlock()
				}
				return
			}
			log.Printf("[job:%s] stream ended (EOF)", jobId)
			if cerr := reconcileGenerationTerminal(ctx, jobId, ajs, ""); cerr != nil {
				loopErr = cerr
				ajs.mu.Lock()
				ajs.phase = streamPhaseFailedPersisting
				ajs.mu.Unlock()
				markJobStreamUnhealthy(jobId)
				return
			}
			return
		}

		if err != nil {
			if getGeneration(jobId) != ajs {
				ajs.mu.Lock()
				ajs.outcome = loopOutcomeSuperseded
				ajs.mu.Unlock()
				return
			}
			if errors.Is(err, io.ErrClosedPipe) || errors.Is(err, context.Canceled) {
				log.Printf("[job:%s] stream closed locally (not marking StreamDone): %v", jobId, err)
				ajs.mu.Lock()
				ajs.outcome = loopOutcomeCanceled
				ajs.mu.Unlock()
				return
			}
			if isWriterTerminalStreamError(err) {
				ajs.mu.Lock()
				phase := ajs.phase
				ajs.terminalErr = err
				ajs.terminalObs = true
				ajs.outcome = loopOutcomeRemoteTerminal
				if phase == streamPhaseActive {
					ajs.terminalPend = true
				}
				ajs.mu.Unlock()
				if phase == streamPhaseCandidate {
					log.Printf("[job:%s] [stream:%s] terminal error while candidate — waiting for Start: %v", jobId, streamId, err)
					_ = ajs.waitStartResolved(ctx)
					ajs.mu.Lock()
					phase = ajs.phase
					startOK := ajs.startOK
					termErr := ajs.terminalErr
					if phase == streamPhaseActive && startOK {
						ajs.terminalPend = true
					}
					ajs.mu.Unlock()
					if phase == streamPhaseActive && startOK {
						msg := ""
						if termErr != nil {
							msg = termErr.Error()
						}
						if cerr := reconcileGenerationTerminal(ctx, jobId, ajs, msg); cerr != nil {
							loopErr = cerr
							ajs.mu.Lock()
							ajs.phase = streamPhaseFailedPersisting
							ajs.mu.Unlock()
							markJobStreamUnhealthy(jobId)
							return
						}
					} else {
						ajs.mu.Lock()
						ajs.terminalPend = false
						ajs.mu.Unlock()
					}
					return
				}
				log.Printf("[job:%s] stream ended with terminal error: %v", jobId, err)
				if cerr := reconcileGenerationTerminal(ctx, jobId, ajs, err.Error()); cerr != nil {
					loopErr = cerr
					ajs.mu.Lock()
					ajs.phase = streamPhaseFailedPersisting
					ajs.mu.Unlock()
					markJobStreamUnhealthy(jobId)
					return
				}
				return
			}
			log.Printf("[job:%s] stream transport error (not marking StreamDone): %v", jobId, err)
			ajs.mu.Lock()
			ajs.outcome = loopOutcomeTransportLost
			ajs.mu.Unlock()
			// Candidate: wait for Start so promote can reject transport-lost.
			ajs.mu.Lock()
			phase := ajs.phase
			ajs.mu.Unlock()
			if phase == streamPhaseCandidate {
				_ = ajs.waitStartResolved(ctx)
			}
			markJobStreamUnhealthy(jobId)
			return
		}
	}
}

// commitStreamDone persists StreamDone to the job row. Failure leaves
// terminalPend so the generation is not cleaned as success.
func commitStreamDone(ctx context.Context, jobId string, gen *streamGeneration, streamErr string) error {
	err := wstore.DBUpdateFn(ctx, jobId, func(job *waveobj.Job) {
		job.StreamDone = true
		if streamErr != "" {
			job.StreamError = streamErr
		}
	})
	if err != nil {
		log.Printf("[job:%s] error updating job stream status: %v", jobId, err)
		if gen != nil {
			gen.mu.Lock()
			gen.terminalPend = true
			if gen.phase == streamPhaseActive {
				gen.phase = streamPhaseFailedPersisting
			}
			gen.mu.Unlock()
		}
		markJobStreamUnhealthy(jobId)
		return fmt.Errorf("%w: stream done commit: %v", ErrStreamPersistence, err)
	}
	return nil
}

// reconcileGenerationTerminal is the only terminal commit entrypoint. It uses
// the generation persistence gate so the output loop and later recovery
// redrive cannot submit the same StreamDone transition concurrently.
func reconcileGenerationTerminal(ctx context.Context, jobId string, gen *streamGeneration, streamErr string) error {
	if gen == nil {
		return nil
	}
	runCtx, cancel := mergePersistenceContext(ctx, gen.ctx)
	defer cancel()
	gate := generationPersistenceGate(gen)
	select {
	case <-gate:
		defer func() { gate <- struct{}{} }()
	case <-runCtx.Done():
		return fmt.Errorf("%w: waiting for terminal persistence owner: %v", ErrStreamPersistence, runCtx.Err())
	}
	gen.mu.Lock()
	termPend := gen.terminalPend
	gen.mu.Unlock()
	if !termPend {
		return nil
	}
	if err := commitStreamDoneFn(runCtx, jobId, gen, streamErr); err != nil {
		return err
	}
	gen.mu.Lock()
	gen.terminalPend = false
	gen.mu.Unlock()
	restoreGenerationActiveIfDurable(jobId, gen)
	tryTerminateJobManager(runCtx, jobId)
	return nil
}

// waitCandidateTerminalResolution never writes terminal state. If a candidate
// observed terminal before Start resolved, the Start path waits for the output
// owner and propagates its exact persistence result.
func waitCandidateTerminalResolution(ctx context.Context, gen *streamGeneration) error {
	if gen == nil {
		return nil
	}
	gen.mu.Lock()
	terminalObserved := gen.terminalObs
	done := gen.done
	gen.mu.Unlock()
	if !terminalObserved || done == nil {
		return nil
	}
	select {
	case <-done:
		return gen.Result()
	case <-ctx.Done():
		return fmt.Errorf("waiting for candidate terminal resolution: %w", ctx.Err())
	}
}

func validateRestartedGeneration(ctx context.Context, jobId string, gen *streamGeneration) error {
	if gen != nil {
		gen.mu.Lock()
		current := getGeneration(jobId) == gen
		healthy := current && isGenerationHealthyLocked(gen)
		terminalObserved := current && gen.terminalObs
		done := gen.done
		gen.mu.Unlock()

		if healthy {
			return nil
		}
		if terminalObserved && done != nil {
			select {
			case <-done:
				if err := gen.Result(); err != nil {
					return fmt.Errorf("restarted generation terminal persistence: %w", err)
				}
				return nil
			case <-ctx.Done():
				return fmt.Errorf("waiting for restarted generation terminal persistence: %w", ctx.Err())
			}
		}
	}
	job, err := wstore.DBMustGet[*waveobj.Job](ctx, jobId)
	if err != nil {
		return fmt.Errorf("failed to validate restarted stream: %w", err)
	}
	if job.StreamDone {
		return nil
	}
	return fmt.Errorf("%w: restarted generation is neither active nor durably terminal", ErrStreamPersistence)
}

func HandleCmdJobExited(ctx context.Context, jobId string, data wshrpc.CommandJobCmdExitedData) error {
	var updatedJob *waveobj.Job
	err := wstore.DBUpdateFn(ctx, jobId, func(job *waveobj.Job) {
		job.CmdExitError = data.ExitErr
		job.CmdExitCode = data.ExitCode
		job.CmdExitSignal = data.ExitSignal
		job.CmdExitTs = data.ExitTs
		updatedJob = job
	})
	if err != nil {
		return fmt.Errorf("failed to update job exit status: %w", err)
	}
	sendBlockJobStatusEventByJob(ctx, updatedJob)
	tryTerminateJobManager(ctx, jobId)

	shouldWrite := jobTerminationMessageWritten.TestAndSet(jobId, true, func(val bool, exists bool) bool {
		return !exists || !val
	})
	if shouldWrite {
		resetTerminalState(ctx, updatedJob.AttachedBlockId)
		msg := "shell terminated"
		if updatedJob.CmdExitCode != nil && *updatedJob.CmdExitCode != 0 {
			msg = fmt.Sprintf("shell terminated (exit code %d)", *updatedJob.CmdExitCode)
		} else if updatedJob.CmdExitSignal != "" {
			msg = fmt.Sprintf("shell terminated (signal %s)", updatedJob.CmdExitSignal)
		}
		writeMutedMessageToTerminal(updatedJob.AttachedBlockId, "["+msg+"]")
	}
	return nil
}

func tryTerminateJobManager(ctx context.Context, jobId string) {
	job, err := wstore.DBMustGet[*waveobj.Job](ctx, jobId)
	if err != nil {
		log.Printf("[job:%s] error getting job for termination check: %v", jobId, err)
		return
	}

	if job.JobManagerStatus != JobManagerStatus_Running {
		return
	}

	cmdExited := job.CmdExitTs != 0

	if !cmdExited || !job.StreamDone {
		log.Printf("[job:%s] not ready for termination: exited=%v streamDone=%v", jobId, cmdExited, job.StreamDone)
		return
	}

	log.Printf("[job:%s] both job cmd exited and stream finished, terminating job manager", jobId)

	err = TerminateJobManager(ctx, jobId)
	if err != nil {
		log.Printf("[job:%s] error terminating job manager: %v", jobId, err)
	}
}

func TerminateAndDetachJob(ctx context.Context, jobId string) {
	err := TerminateJobManager(ctx, jobId)
	if err != nil {
		log.Printf("[job:%s] error terminating job manager: %v", jobId, err)
	}
	err = DetachJobFromBlock(ctx, jobId, true)
	if err != nil {
		log.Printf("[job:%s] error detaching job from block: %v", jobId, err)
	}
}

func TerminateJobManager(ctx context.Context, jobId string) error {
	_, err, _ := terminateJobManagerGroup.Do(jobId, func() (any, error) {
		err := doTerminateJobManager(ctx, jobId)
		return nil, err
	})
	return err
}

func doTerminateJobManager(ctx context.Context, jobId string) error {
	var shouldTerminate bool
	var job *waveobj.Job
	err := wstore.DBUpdateFn(ctx, jobId, func(j *waveobj.Job) {
		job = j
		if j.JobManagerStatus == JobManagerStatus_Done {
			shouldTerminate = false
			return
		}
		j.TerminateOnReconnect = true
		shouldTerminate = true
	})
	if err != nil {
		return fmt.Errorf("failed to set TerminateOnReconnect: %w", err)
	}

	if !shouldTerminate {
		log.Printf("[job:%s] already terminated, skipping", jobId)
		return nil
	}

	return remoteTerminateJobManager(ctx, job)
}

func DisconnectJob(ctx context.Context, jobId string) error {
	job, err := wstore.DBMustGet[*waveobj.Job](ctx, jobId)
	if err != nil {
		return fmt.Errorf("failed to get job: %w", err)
	}

	bareRpc := wshclient.GetBareRpcClient()
	rpcOpts := &wshrpc.RpcOpts{
		Route:   wshutil.MakeConnectionRouteId(job.Connection),
		Timeout: 5000,
	}

	disconnectData := wshrpc.CommandRemoteDisconnectFromJobManagerData{
		JobId: jobId,
	}

	err = wshclient.RemoteDisconnectFromJobManagerCommand(bareRpc, disconnectData, rpcOpts)
	if err != nil {
		return fmt.Errorf("failed to send disconnect command: %w", err)
	}

	log.Printf("[job:%s] job disconnect command sent successfully", jobId)
	return nil
}

func remoteTerminateJobManager(ctx context.Context, job *waveobj.Job) error {
	log.Printf("[job:%s] terminating job manager", job.OID)

	shouldWrite := jobTerminationMessageWritten.TestAndSet(job.OID, true, func(val bool, exists bool) bool {
		return !exists || !val
	})
	if shouldWrite {
		resetTerminalState(ctx, job.AttachedBlockId)
		writeMutedMessageToTerminal(job.AttachedBlockId, "[shell terminated]")
	}

	if job.JobManagerStatus == JobManagerStatus_Done {
		log.Printf("[job:%s] job manager already marked as done, skipping termination", job.OID)
		return nil
	}

	bareRpc := wshclient.GetBareRpcClient()
	terminateData := wshrpc.CommandRemoteTerminateJobManagerData{
		JobId:             job.OID,
		JobManagerPid:     job.JobManagerPid,
		JobManagerStartTs: job.JobManagerStartTs,
	}

	rpcOpts := &wshrpc.RpcOpts{
		Route:   wshutil.MakeConnectionRouteId(job.Connection),
		Timeout: 5000,
	}

	err := wshclient.RemoteTerminateJobManagerCommand(bareRpc, terminateData, rpcOpts)
	if err != nil {
		log.Printf("[job:%s] error terminating job manager: %v", job.OID, err)
		return fmt.Errorf("failed to terminate job manager: %w", err)
	}

	var updatedJob *waveobj.Job
	updateErr := wstore.DBUpdateFn(ctx, job.OID, func(job *waveobj.Job) {
		job.JobManagerStatus = JobManagerStatus_Done
		job.JobManagerDoneReason = JobDoneReason_Terminated
		job.TerminateOnReconnect = false
		if !job.StreamDone {
			job.StreamDone = true
			job.StreamError = "job manager terminated"
		}
		updatedJob = job
	})
	if updateErr != nil {
		log.Printf("[job:%s] error updating job status after termination: %v", job.OID, updateErr)
	} else {
		sendBlockJobStatusEventByJob(ctx, updatedJob)
	}

	telemetry.GoRecordTEventWrap(&telemetrydata.TEvent{
		Event: "job:done",
		Props: telemetrydata.TEventProps{
			JobDoneReason: JobDoneReason_Terminated,
			JobKind:       job.JobKind,
		},
	})

	log.Printf("[job:%s] job manager terminated successfully", job.OID)
	return nil
}

func ReconnectJob(ctx context.Context, jobId string, rtOpts *waveobj.RuntimeOpts) error {
	_, err, _ := reconnectGroup.Do(jobId, func() (any, error) {
		return nil, doReconnectJob(ctx, jobId, rtOpts)
	})
	return err
}

func doReconnectJob(ctx context.Context, jobId string, rtOpts *waveobj.RuntimeOpts) error {
	if isJobDeletingOrDeleted(jobId) {
		return fmt.Errorf("%w", ErrJobDeleted)
	}
	job, err := wstore.DBMustGet[*waveobj.Job](ctx, jobId)
	if err != nil {
		if errors.Is(err, wstore.ErrNotFound) {
			return fmt.Errorf("%w: %w", ErrJobDeleted, err)
		}
		// Other DB errors are retryable — do not wrap as terminal.
		return fmt.Errorf("failed to get job: %w", err)
	}

	_, err = CheckJobConnected(ctx, jobId)
	if err == nil {
		log.Printf("[job:%s] already connected with active stream, skipping reconnect", jobId)
		return nil
	}
	log.Printf("[job:%s] not healthy, proceeding with reconnect: %v", jobId, err)

	isConnected, err := conncontroller.IsConnected(job.Connection)
	if err != nil {
		return fmt.Errorf("error checking connection status: %w", err)
	}
	if !isConnected {
		return fmt.Errorf("connection %q is not connected", job.Connection)
	}

	if job.TerminateOnReconnect {
		return remoteTerminateJobManager(ctx, job)
	}

	if rtOpts == nil {
		rtOpts = &waveobj.RuntimeOpts{
			TermSize: job.CmdTermSize,
		}
	}

	// Stream-only recovery: job route may still be up but we have no active reader.
	// Skip full RemoteReconnect when the job route is already registered.
	// Failures return to the recovery owner loop — do NOT self re-queue.
	routeId := wshutil.MakeJobRouteId(jobId)
	routeEpoch := getJobRouteEpoch(jobId)
	routeCheckCtx, routeCheckCancel := context.WithTimeout(ctx, 50*time.Millisecond)
	routeAlreadyUp := wshutil.DefaultRouter.WaitForRegister(routeCheckCtx, routeId) == nil
	routeCheckCancel()
	if routeAlreadyUp {
		routeAlreadyUp = confirmJobRouteUp(jobId, routeEpoch)
	}
	if routeAlreadyUp && !isHealthyActiveGeneration(jobId) && !job.StreamDone {
		log.Printf("[job:%s] job route still registered, attempting stream-only restart", jobId)
		// Do not publish Connected until restartStreaming promotes an active generation.
		markJobStreamUnhealthy(jobId)
		if err := restartStreaming(ctx, jobId, true, rtOpts); err != nil {
			log.Printf("[job:%s] stream-only restart failed: %v", jobId, err)
			markJobStreamUnhealthy(jobId)
			return err
		}
		if !publishConnectedIfAllowed(jobId) || isJobDeletingOrDeleted(jobId) {
			markJobStreamUnhealthy(jobId)
			if isJobDeletingOrDeleted(jobId) {
				return fmt.Errorf("%w: job deleted during reconnect", ErrJobDeleted)
			}
			return fmt.Errorf("job route went down before Connected publish")
		}
		sendBlockJobStatusEventByJob(ctx, job)
		telemetry.GoRecordTEventWrap(&telemetrydata.TEvent{
			Event: "job:reconnect",
			Props: telemetrydata.TEventProps{
				JobKind: job.JobKind,
			},
		})
		return nil
	}

	bareRpc := wshclient.GetBareRpcClient()

	jobAccessClaims := &wavejwt.WaveJwtClaims{
		MainServer: true,
		JobId:      jobId,
	}
	jobAccessToken, err := wavejwt.Sign(jobAccessClaims)
	if err != nil {
		return fmt.Errorf("failed to generate job access token: %w", err)
	}

	reconnectData := wshrpc.CommandRemoteReconnectToJobManagerData{
		JobId:              jobId,
		JobAuthToken:       job.JobAuthToken,
		MainServerJwtToken: jobAccessToken,
		JobManagerPid:      job.JobManagerPid,
		JobManagerStartTs:  job.JobManagerStartTs,
	}

	rpcOpts := &wshrpc.RpcOpts{
		Route:   wshutil.MakeConnectionRouteId(job.Connection),
		Timeout: 5000,
	}

	log.Printf("[job:%s] sending RemoteReconnectToJobManagerCommand to connection %s", jobId, job.Connection)
	rtnData, err := wshclient.RemoteReconnectToJobManagerCommand(bareRpc, reconnectData, rpcOpts)
	if err != nil {
		log.Printf("[job:%s] RemoteReconnectToJobManagerCommand failed: %v", jobId, err)
		markJobStreamUnhealthy(jobId)
		return fmt.Errorf("failed to reconnect to job manager: %w", err)
	}

	if !rtnData.Success {
		log.Printf("[job:%s] RemoteReconnectToJobManagerCommand returned error: %s", jobId, rtnData.Error)
		if rtnData.JobManagerGone {
			var updatedJob *waveobj.Job
			updateErr := wstore.DBUpdateFn(ctx, jobId, func(job *waveobj.Job) {
				job.JobManagerStatus = JobManagerStatus_Done
				job.JobManagerDoneReason = JobDoneReason_Gone
				updatedJob = job
			})
			if updateErr != nil {
				// DB not durable yet — retryable, not terminal. Owner must keep trying.
				log.Printf("[job:%s] error updating job manager gone status (will retry): %v", jobId, updateErr)
				markJobStreamUnhealthy(jobId)
				return fmt.Errorf("failed to persist JobManagerGone: %w", updateErr)
			}
			sendBlockJobStatusEventByJob(ctx, updatedJob)
			telemetry.GoRecordTEventWrap(&telemetrydata.TEvent{
				Event: "job:done",
				Props: telemetrydata.TEventProps{
					JobDoneReason: JobDoneReason_Gone,
					JobKind:       job.JobKind,
				},
			})
			writeJobTerminationMessage(ctx, jobId, updatedJob, "[session gone]")
			return fmt.Errorf("%w: %s", ErrJobManagerGone, rtnData.Error)
		}
		markJobStreamUnhealthy(jobId)
		return fmt.Errorf("failed to reconnect to job manager: %s", rtnData.Error)
	}

	log.Printf("[job:%s] RemoteReconnectToJobManagerCommand succeeded, waiting for route", jobId)

	routeEpoch = getJobRouteEpoch(jobId)
	waitCtx, cancelFn := context.WithTimeout(ctx, 2*time.Second)
	defer cancelFn()
	err = wshutil.DefaultRouter.WaitForRegister(waitCtx, routeId)
	if err != nil {
		markJobStreamUnhealthy(jobId)
		return fmt.Errorf("route did not establish after successful reconnection: %w", err)
	}
	if !confirmJobRouteUp(jobId, routeEpoch) {
		markJobStreamUnhealthy(jobId)
		return fmt.Errorf("job route went down while reconnecting")
	}

	// Publish Connected only after streaming is installed (or stream is done).
	// Failures return to the recovery owner loop — do NOT self re-queue.
	log.Printf("[job:%s] route established, restarting streaming before publishing Connected", jobId)
	if err := restartStreaming(ctx, jobId, true, rtOpts); err != nil {
		log.Printf("[job:%s] restartStreaming failed after route up: %v", jobId, err)
		markJobStreamUnhealthy(jobId)
		return err
	}
	// restartStreaming promotes active generation OR observes StreamDone — mark Connected.
	if !publishConnectedIfAllowed(jobId) || isJobDeletingOrDeleted(jobId) {
		markJobStreamUnhealthy(jobId)
		if isJobDeletingOrDeleted(jobId) {
			return fmt.Errorf("%w: job deleted during reconnect", ErrJobDeleted)
		}
		return fmt.Errorf("job route went down before Connected publish")
	}
	sendBlockJobStatusEventByJob(ctx, job)

	telemetry.GoRecordTEventWrap(&telemetrydata.TEvent{
		Event: "job:reconnect",
		Props: telemetrydata.TEventProps{
			JobKind: job.JobKind,
		},
	})
	return nil
}

func ReconnectJobsForConn(ctx context.Context, connName string) error {
	isConnected, err := conncontroller.IsConnected(connName)
	if err != nil {
		return fmt.Errorf("error checking connection status: %w", err)
	}
	if !isConnected {
		return fmt.Errorf("connection %q is not connected", connName)
	}

	allJobs, err := wstore.DBGetAllObjsByType[*waveobj.Job](ctx, waveobj.OType_Job)
	if err != nil {
		return fmt.Errorf("failed to get jobs: %w", err)
	}

	var jobsToReconnect []*waveobj.Job
	for _, job := range allJobs {
		if job.Connection == connName && isJobManagerRunning(job) {
			jobsToReconnect = append(jobsToReconnect, job)
		}
	}

	log.Printf("[conn:%s] found %d jobs to reconnect via recovery coordinator", connName, len(jobsToReconnect))

	for _, job := range jobsToReconnect {
		wakeStreamRecovery(job.OID)
		requestStreamRecovery(job.OID, connName, "reconnect-jobs-for-conn", true)
	}

	return nil
}

func restartStreaming(ctx context.Context, jobId string, knownConnected bool, rtOpts *waveobj.RuntimeOpts) error {
	if isJobDeletingOrDeleted(jobId) {
		return fmt.Errorf("%w", ErrJobDeleted)
	}
	startTime := time.Now()
	job, err := wstore.DBMustGet[*waveobj.Job](ctx, jobId)
	if err != nil {
		return fmt.Errorf("failed to get job: %w", err)
	}

	termSize := job.CmdTermSize
	if rtOpts != nil && rtOpts.TermSize.Rows > 0 && rtOpts.TermSize.Cols > 0 {
		termSize = rtOpts.TermSize
		err = wstore.DBUpdateFn(ctx, jobId, func(job *waveobj.Job) {
			job.CmdTermSize = termSize
		})
		if err != nil {
			log.Printf("[job:%s] warning: failed to update termsize in DB: %v", jobId, err)
		}
	}

	if !knownConnected {
		isConnected, err := conncontroller.IsConnected(job.Connection)
		if err != nil {
			return fmt.Errorf("error checking connection status: %w", err)
		}
		if !isConnected {
			return fmt.Errorf("connection %q is not connected", job.Connection)
		}

		jobConnStatus := GetJobConnStatus(jobId)
		if jobConnStatus != JobConnStatus_Connected {
			return fmt.Errorf("job manager is not connected (status: %s)", jobConnStatus)
		}
	}

	// Quiesce the old output loop BEFORE snapshotting WaveFS size so it cannot
	// append more bytes after we compute the resume sequence. Persistence
	// failure / timeout forbids snapshot and starting a new generation.
	oldStreamId := getActiveStreamId(jobId)
	log.Printf("[job:%s] durable stream reconnect started (oldStreamId=%s)", jobId, oldStreamId)
	if err := quiesceActiveJobStream(jobId, OldStreamQuiesceTimeout); err != nil {
		markJobStreamUnhealthy(jobId)
		return fmt.Errorf("failed to quiesce old stream before reconnect: %w", err)
	}

	var currentSeq int64 = 0
	var totalGap int64 = 0
	var fileSize int64 = 0
	waveFile, err := filestore.WFS.Stat(ctx, jobId, JobOutputFileName)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			markJobStreamUnhealthy(jobId)
			return fmt.Errorf("failed to stat WaveFS output for resume: %w", err)
		}
		// Missing file is a valid cold start (size 0).
	} else {
		fileSize = waveFile.Size
		currentSeq = waveFile.Size
		totalGap = getMetaInt64(waveFile.Meta, MetaKey_TotalGap)
		currentSeq += totalGap
	}

	bareRpc := wshclient.GetBareRpcClient()
	broker := bareRpc.StreamBroker
	readerRouteId := wshclient.GetBareRpcClientRouteId()
	writerRouteId := wshutil.MakeJobRouteId(jobId)
	// Reader may receive/ACK data as soon as remote StartStream runs; candidate
	// generation + output loop must own persistence BEFORE Start RPC.
	reader, streamMeta := broker.CreateStreamReaderWithSeq(readerRouteId, writerRouteId, DefaultStreamRwnd, currentSeq)

	prepareData := wshrpc.CommandJobPrepareConnectData{
		StreamMeta: *streamMeta,
		Seq:        currentSeq,
		TermSize:   termSize,
	}

	rpcOpts := &wshrpc.RpcOpts{
		Route:   wshutil.MakeJobRouteId(jobId),
		Timeout: 5000,
	}

	log.Printf("[job:%s] sending JobPrepareConnectCommand with seq=%d (fileSize=%d, totalGap=%d)", jobId, currentSeq, fileSize, totalGap)
	rtnData, err := wshclient.JobPrepareConnectCommand(bareRpc, prepareData, rpcOpts)
	if err != nil {
		// Prepare uses RWnd=0 corked stream — no data should have been ACKed.
		_ = reader.Close()
		markJobStreamUnhealthy(jobId)
		return fmt.Errorf("failed to prepare connect: %w", err)
	}

	if rtnData.HasExited {
		exitCodeStr := "nil"
		if rtnData.ExitCode != nil {
			exitCodeStr = fmt.Sprintf("%d", *rtnData.ExitCode)
		}
		log.Printf("[job:%s] job has already exited: code=%s signal=%q err=%q", jobId, exitCodeStr, rtnData.ExitSignal, rtnData.ExitErr)
		exitData := wshrpc.CommandJobCmdExitedData{
			ExitCode:   rtnData.ExitCode,
			ExitSignal: rtnData.ExitSignal,
			ExitErr:    rtnData.ExitErr,
			ExitTs:     time.Now().UnixMilli(),
		}
		if exitErr := HandleCmdJobExited(ctx, jobId, exitData); exitErr != nil {
			_ = reader.Close()
			markJobStreamUnhealthy(jobId)
			return fmt.Errorf("failed to persist job exit status: %w", exitErr)
		}
	}

	if rtnData.StreamDone {
		log.Printf("[job:%s] stream is already done: error=%q", jobId, rtnData.StreamError)
		updateErr := wstore.DBUpdateFn(ctx, jobId, func(job *waveobj.Job) {
			if !job.StreamDone {
				job.StreamDone = true
				if rtnData.StreamError != "" {
					job.StreamError = rtnData.StreamError
				}
			}
		})
		if updateErr != nil {
			_ = reader.Close()
			markJobStreamUnhealthy(jobId)
			return fmt.Errorf("failed to persist StreamDone from prepare: %w", updateErr)
		}
	}

	if rtnData.StreamDone && rtnData.HasExited {
		_ = reader.Close()
		log.Printf("[job:%s] both stream done and job exited, calling tryExitJobManager", jobId)
		tryTerminateJobManager(ctx, jobId)
		return nil
	}

	if rtnData.StreamDone {
		_ = reader.Close()
		log.Printf("[job:%s] stream already done, no need to restart streaming", jobId)
		return nil
	}

	gapDetected := false
	if rtnData.Seq > currentSeq {
		gap := rtnData.Seq - currentSeq
		totalGap += gap
		gapDetected = true
		log.Printf("[job:%s] detected gap: our seq=%d, server seq=%d, gap=%d, new totalGap=%d", jobId, currentSeq, rtnData.Seq, gap, totalGap)

		metaErr := filestore.WFS.WriteMeta(ctx, jobId, JobOutputFileName, wshrpc.FileMeta{
			MetaKey_TotalGap: totalGap,
		}, true)
		if metaErr != nil {
			_ = reader.Close()
			markJobStreamUnhealthy(jobId)
			return fmt.Errorf("failed to write totalgap metadata before seq advance: %w", metaErr)
		}

		reader.UpdateNextSeq(rtnData.Seq)
	}

	// Candidate + persistence owner BEFORE StartStream (remote may open window
	// and send data even if the RPC response is lost/timeout).
	gen, err := createCandidate(jobId, streamMeta.Id, reader)
	if err != nil {
		_ = reader.Close()
		markJobStreamUnhealthy(jobId)
		return fmt.Errorf("failed to create candidate generation: %w", err)
	}
	go func() {
		defer func() {
			panichandler.PanicHandler("jobcontroller:RestartStreaming:runOutputLoop", recover())
		}()
		runOutputLoop(gen.ctx, jobId, gen)
	}()

	log.Printf("[job:%s] sending JobStartStreamCommand (candidate=%s)", jobId, streamMeta.Id)
	startStreamData := wshrpc.CommandJobStartStreamData{}
	err = wshclient.JobStartStreamCommand(bareRpc, startStreamData, rpcOpts)
	if err != nil {
		// Treat as ambiguous remote success: remote may already be streaming.
		// Clear unpromoted terminal commit requirement so drain can remove candidate.
		log.Printf("[job:%s] JobStartStreamCommand failed (ambiguous); draining candidate: %v", jobId, err)
		gen.mu.Lock()
		gen.terminalPend = false
		gen.mu.Unlock()
		gen.signalStartResolved(false)
		_ = beginGenerationDrain(jobId, gen)
		drainCtx, drainCancel := context.WithTimeout(context.Background(), OldStreamQuiesceTimeout)
		perr := waitGenerationPersisted(drainCtx, jobId, gen)
		drainCancel()
		if perr != nil {
			// Force-remove if only terminal-obs blocked; primary pending must remain.
			if gen.PendingLen() == 0 {
				gen.mu.Lock()
				gen.terminalPend = false
				gen.terminalObs = false
				gen.mu.Unlock()
				removeGenerationIf(jobId, gen)
			}
			markJobStreamUnhealthy(jobId)
			return fmt.Errorf("failed to start stream (and candidate drain failed): %w: %v", err, perr)
		}
		// Drain succeeded — pointer-safe remove.
		gen.mu.Lock()
		gen.terminalPend = false
		gen.terminalObs = false
		gen.mu.Unlock()
		removeGenerationIf(jobId, gen)
		markJobStreamUnhealthy(jobId)
		return fmt.Errorf("failed to start stream: %w", err)
	}

	if err := promoteCandidate(jobId, streamMeta.Id); err != nil {
		gen.signalStartResolved(false)
		markJobStreamUnhealthy(jobId)
		return fmt.Errorf("failed to promote candidate: %w", err)
	}
	gen.signalStartResolved(true)
	// Start never commits terminal state. If EOF/error raced the Start response,
	// wait for the output owner and propagate its exact durable result.
	if err := waitCandidateTerminalResolution(ctx, gen); err != nil {
		markJobStreamUnhealthy(jobId)
		return fmt.Errorf("candidate terminal persistence failed: %w", err)
	}
	if err := validateRestartedGeneration(ctx, jobId, gen); err != nil {
		markJobStreamUnhealthy(jobId)
		return err
	}

	resumeSeq := currentSeq
	if rtnData.Seq > resumeSeq {
		resumeSeq = rtnData.Seq
	}

	log.Printf("[job:%s] durable stream resumed from seq=%d oldStreamId=%s newStreamId=%s gap=%v elapsed=%s",
		jobId, resumeSeq, oldStreamId, streamMeta.Id, gapDetected, time.Since(startTime))
	log.Printf("[job:%s] streaming restarted successfully", jobId)
	return nil
}

// this function must be kept up to date with getBlockTermDurableAtom in frontend/app/store/global.ts
func IsBlockTermDurable(block *waveobj.Block) bool {
	if block == nil {
		return false
	}

	// Check if view is "term", and controller is "shell"
	if block.Meta.GetString(waveobj.MetaKey_View, "") != "term" || block.Meta.GetString(waveobj.MetaKey_Controller, "") != "shell" {
		return false
	}

	// 1. Check if block has a JobId
	if block.JobId != "" {
		return true
	}

	// 2. Check if connection is local or WSL (not durable)
	connName := block.Meta.GetString(waveobj.MetaKey_Connection, "")
	if conncontroller.IsLocalConnName(connName) || conncontroller.IsWslConnName(connName) {
		return false
	}

	// 3. Check config hierarchy: blockmeta → connection → global (default true)
	// Check block meta first
	if val, exists := block.Meta[waveobj.MetaKey_TermDurable]; exists {
		if boolVal, ok := val.(bool); ok {
			return boolVal
		}
	}
	// Check connection config
	fullConfig := wconfig.GetWatcher().GetFullConfig()
	if connName != "" {
		if connConfig, exists := fullConfig.Connections[connName]; exists {
			if connConfig.TermDurable != nil {
				return *connConfig.TermDurable
			}
		}
	}
	// Check global settings
	if fullConfig.Settings.TermDurable != nil {
		return *fullConfig.Settings.TermDurable
	}
	// Default to true for non-local connections
	return true
}

func IsBlockIdTermDurable(blockId string) bool {
	block, err := wstore.DBGet[*waveobj.Block](context.Background(), blockId)
	if err != nil || block == nil {
		return false
	}
	return IsBlockTermDurable(block)
}

func DeleteJob(ctx context.Context, jobId string) error {
	// Real tombstone first: recovery / createCandidate / promote / Connected
	// must observe deleting and refuse new work before we cancel owners.
	if !markJobDeleting(jobId) {
		// Already deleted — treat as success.
		return nil
	}
	SetJobConnStatus(jobId, JobConnStatus_Disconnected)
	jobTerminationMessageWritten.Delete(jobId)
	lastAutoReconnectAttempt.Delete(jobId)

	// Cancel recovery owner and wait for exit.
	recoveryCtx, recoveryCancel := context.WithTimeout(ctx, StreamRecoveryAttemptTimeout+2*time.Second)
	recoveryErr := cancelStreamRecovery(recoveryCtx, jobId)
	recoveryCancel()
	if recoveryErr != nil {
		log.Printf("[job:%s] DeleteJob: recovery owner did not exit: %v", jobId, recoveryErr)
		return recoveryErr
	}

	// Cancel and wait for output owner before deleting storage.
	if gen := getGeneration(jobId); gen != nil {
		if gen.cancel != nil {
			gen.cancel()
		}
		if gen.reader != nil {
			_ = gen.reader.Close() // hard cancel — no further buffer emit
		}
		waitCtx, waitCancel := context.WithTimeout(context.Background(), 2*time.Second)
		waitErr := waitGenerationDone(waitCtx, gen)
		if waitErr == nil {
			waitErr = waitMirrorRepairDone(waitCtx, gen)
		}
		waitCancel()
		if waitErr != nil {
			// Do not delete WaveFS/DB while the output owner may still write.
			log.Printf("[job:%s] DeleteJob: output owner did not exit in time: %v", jobId, waitErr)
			// Leave tombstone (deleting) so recovery cannot recreate; surface error.
			return fmt.Errorf("delete job %s: output owner still running: %w", jobId, waitErr)
		}
		// Force discard even with pending — job is being deleted and owner exited.
		activeJobStreams.DeleteIf(jobId, func(cur *streamGeneration, exists bool) bool {
			return exists && cur == gen
		})
	}

	err := filestore.WFS.DeleteZone(ctx, jobId)
	if err != nil {
		log.Printf("[job:%s] warning: error deleting WaveFS zone: %v", jobId, err)
	}
	if err := wstore.DBDelete(ctx, waveobj.OType_Job, jobId); err != nil {
		return err
	}
	markJobDeleted(jobId)
	return nil
}

func AttachJobToBlock(ctx context.Context, jobId string, blockId string) error {
	err := wstore.WithTx(ctx, func(tx *wstore.TxWrap) error {
		var oldJobId string

		err := wstore.DBUpdateFn(tx.Context(), blockId, func(block *waveobj.Block) {
			oldJobId = block.JobId
			block.JobId = jobId
		})
		if err != nil {
			return fmt.Errorf("failed to update block: %w", err)
		}

		if oldJobId != "" && oldJobId != jobId {
			err = wstore.DBUpdateFn(tx.Context(), oldJobId, func(oldJob *waveobj.Job) {
				if oldJob.AttachedBlockId == blockId {
					oldJob.AttachedBlockId = ""
				}
			})
			if err != nil {
				log.Printf("[job:%s] warning: could not detach old job: %v", oldJobId, err)
			}
		}

		err = wstore.DBUpdateFnErr(tx.Context(), jobId, func(job *waveobj.Job) error {
			if job.AttachedBlockId != "" && job.AttachedBlockId != blockId {
				return fmt.Errorf("job %s already attached to block %s", jobId, job.AttachedBlockId)
			}
			job.AttachedBlockId = blockId
			return nil
		})
		if err != nil {
			return fmt.Errorf("failed to update job: %w", err)
		}

		log.Printf("[job:%s] attached to block:%s", jobId, blockId)
		return nil
	})
	if err != nil {
		return err
	}

	SendBlockJobStatusEvent(ctx, blockId)
	wcore.SendWaveObjUpdate(waveobj.MakeORef(waveobj.OType_Block, blockId))
	return nil
}

func DetachJobFromBlock(ctx context.Context, jobId string, updateBlock bool) error {
	var blockId string
	var blockUpdated bool
	err := wstore.WithTx(ctx, func(tx *wstore.TxWrap) error {
		job, err := wstore.DBMustGet[*waveobj.Job](tx.Context(), jobId)
		if err != nil {
			return fmt.Errorf("failed to get job: %w", err)
		}

		blockId = job.AttachedBlockId
		if blockId == "" {
			return nil
		}

		if updateBlock {
			block, err := wstore.DBGet[*waveobj.Block](tx.Context(), blockId)
			if err == nil && block != nil {
				err = wstore.DBUpdateFn(tx.Context(), blockId, func(block *waveobj.Block) {
					block.JobId = ""
				})
				if err != nil {
					log.Printf("[job:%s] warning: failed to clear JobId from block:%s: %v", jobId, blockId, err)
				} else {
					blockUpdated = true
				}
			}
		}

		err = wstore.DBUpdateFn(tx.Context(), jobId, func(job *waveobj.Job) {
			job.AttachedBlockId = ""
		})
		if err != nil {
			return fmt.Errorf("failed to update job: %w", err)
		}

		log.Printf("[job:%s] detached from block:%s", jobId, blockId)
		return nil
	})
	if err != nil {
		return err
	}

	if blockId != "" {
		SendBlockJobStatusEvent(ctx, blockId)
		if blockUpdated {
			wcore.SendWaveObjUpdate(waveobj.MakeORef(waveobj.OType_Block, blockId))
		}
	}

	return nil
}

func SendInput(ctx context.Context, data wshrpc.CommandJobInputData) error {
	jobId := data.JobId

	if data.TermSize != nil {
		err := wstore.DBUpdateFn(ctx, jobId, func(job *waveobj.Job) {
			job.CmdTermSize = *data.TermSize
		})
		if err != nil {
			log.Printf("[job:%s] warning: failed to update termsize in DB: %v", jobId, err)
		}
	}

	_, err := CheckJobConnected(ctx, jobId)
	if err != nil {
		return err
	}

	rpcOpts := &wshrpc.RpcOpts{
		Route:      wshutil.MakeJobRouteId(jobId),
		Timeout:    5000,
		NoResponse: false,
	}

	bareRpc := wshclient.GetBareRpcClient()
	err = wshclient.JobInputCommand(bareRpc, data, rpcOpts)
	if err != nil {
		return fmt.Errorf("failed to send input to job: %w", err)
	}

	return nil
}

func resetTerminalState(logCtx context.Context, blockId string) {
	if blockId == "" {
		return
	}
	ctx, cancelFn := context.WithTimeout(context.Background(), DefaultTimeout)
	defer cancelFn()
	if isFileEmpty(ctx, blockId) {
		return
	}
	blocklogger.Debugf(logCtx, "[conndebug] resetTerminalState: resetting terminal state for block\n")
	resetSeq := shellutil.GetTerminalResetSeq()
	resetSeq += "\r\n"
	err := doWFSAppend(ctx, waveobj.MakeORef(waveobj.OType_Block, blockId), JobOutputFileName, []byte(resetSeq))
	if err != nil {
		log.Printf("error appending terminal reset to block file: %v\n", err)
	}
}

func isFileEmpty(ctx context.Context, blockId string) bool {
	if blockId == "" {
		return true
	}
	file, statErr := filestore.WFS.Stat(ctx, blockId, JobOutputFileName)
	if statErr == fs.ErrNotExist {
		return true
	}
	if statErr != nil {
		log.Printf("error statting block output file: %v\n", statErr)
		return true
	}
	return file.Size == 0
}

func writeSessionSeparatorToTerminal(blockId string, termWidth int) {
	if blockId == "" {
		return
	}
	ctx, cancelFn := context.WithTimeout(context.Background(), DefaultTimeout)
	defer cancelFn()
	if isFileEmpty(ctx, blockId) {
		return
	}
	separatorLine := "\r\n"
	err := doWFSAppend(ctx, waveobj.MakeORef(waveobj.OType_Block, blockId), JobOutputFileName, []byte(separatorLine))
	if err != nil {
		log.Printf("error writing session separator to terminal (blockid=%s): %v", blockId, err)
	}
}

// msg should not have a terminating newline
func writeMutedMessageToTerminal(blockId string, msg string) {
	if blockId == "" {
		return
	}
	ctx, cancelFn := context.WithTimeout(context.Background(), DefaultTimeout)
	defer cancelFn()
	fullMsg := "\x1b[90m" + msg + "\x1b[0m\r\n"
	err := doWFSAppend(ctx, waveobj.MakeORef(waveobj.OType_Block, blockId), JobOutputFileName, []byte(fullMsg))
	if err != nil {
		log.Printf("error writing muted message to terminal (blockid=%s): %v", blockId, err)
	}
}

func writeJobTerminationMessage(ctx context.Context, jobId string, job *waveobj.Job, msg string) {
	if job == nil {
		return
	}
	shouldWrite := jobTerminationMessageWritten.TestAndSet(jobId, true, func(val bool, exists bool) bool {
		return !exists || !val
	})
	if shouldWrite {
		resetTerminalState(ctx, job.AttachedBlockId)
		writeMutedMessageToTerminal(job.AttachedBlockId, msg)
	}
}
