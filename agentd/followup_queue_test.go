package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type followupNopCloser struct{ bytes.Buffer }

func (followupNopCloser) Close() error { return nil }

func followupTestTask(t *testing.T, status taskStatus) *task {
	t.Helper()
	cfg := testConfig(t)
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	current := addSettledSourceTask(t, manager, cfg)
	current.mu.Lock()
	current.metadata.Status = status
	current.metadata.UpdatedAt = time.Now().UTC()
	current.mu.Unlock()
	if err := current.saveMetadata(); err != nil {
		t.Fatal(err)
	}
	return current
}

func TestFollowupEnqueueDoesNotCreatePrematureUserMessage(t *testing.T) {
	current := followupTestTask(t, statusRunning)
	item, err := current.enqueueFollowup(followupEnqueueParams{TaskID: current.metadata.ID, Prompt: "after this turn", IdempotencyKey: "request-1"})
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != followupQueued {
		t.Fatalf("status = %s, want queued", item.Status)
	}
	page, err := current.eventPage(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range page.Events {
		if event.Normalized != nil && event.Normalized.Type == "user.message" {
			t.Fatal("enqueue created a user.message before worker delivery")
		}
	}
	if len(page.Events) != 1 || page.Events[0].Normalized == nil || page.Events[0].Normalized.Type != "followup.queued" {
		t.Fatalf("unexpected queue timeline: %+v", page.Events)
	}

	duplicate, err := current.enqueueFollowup(followupEnqueueParams{TaskID: current.metadata.ID, Prompt: "after this turn", IdempotencyKey: "request-1"})
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.ID != item.ID {
		t.Fatalf("idempotency returned %s, want %s", duplicate.ID, item.ID)
	}
}

func TestFollowupQueueFIFOCancelAndPayloadLimit(t *testing.T) {
	current := followupTestTask(t, statusWaiting)
	first, err := current.enqueueFollowup(followupEnqueueParams{TaskID: current.metadata.ID, Prompt: "first"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := current.enqueueFollowup(followupEnqueueParams{TaskID: current.metadata.ID, Prompt: "second"})
	if err != nil {
		t.Fatal(err)
	}
	if err := current.cancelFollowup(first.ID); err != nil {
		t.Fatal(err)
	}
	queue, err := current.readFollowupQueue()
	if err != nil {
		t.Fatal(err)
	}
	if len(queue.Items) != 2 || queue.Items[0].ID != second.ID || queue.Items[1].Status != followupCancelled {
		t.Fatalf("queue after cancellation = %+v", queue.Items)
	}
	limitTask := followupTestTask(t, statusWaiting)
	if _, err := limitTask.enqueueFollowup(followupEnqueueParams{TaskID: limitTask.metadata.ID, Prompt: string(bytes.Repeat([]byte{'x'}, maximumFollowupText)), Images: nil}); err != nil {
		t.Fatalf("exact text limit rejected: %v", err)
	}
	if _, err := limitTask.enqueueFollowup(followupEnqueueParams{TaskID: limitTask.metadata.ID, Prompt: "over"}); err == nil {
		t.Fatal("expected queue text limit after exact-limit message")
	}
}

func TestFollowupQueueConcurrentEnqueueIsBounded(t *testing.T) {
	current := followupTestTask(t, statusRunning)
	var group sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for index := 0; index < maximumFollowupItems+5; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			if _, err := current.enqueueFollowup(followupEnqueueParams{TaskID: current.metadata.ID, Prompt: "concurrent-" + string(rune('a'+index))}); err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}(index)
	}
	group.Wait()
	if successes != maximumFollowupItems {
		t.Fatalf("successful concurrent enqueues = %d, want %d", successes, maximumFollowupItems)
	}
	queue, err := current.readFollowupQueue()
	if err != nil {
		t.Fatal(err)
	}
	if len(queue.Items) != maximumFollowupItems {
		t.Fatalf("persisted concurrent queue length = %d, want %d", len(queue.Items), maximumFollowupItems)
	}
}

func TestFollowupQueueMalformedAndSymlinkFailClosed(t *testing.T) {
	if utf8ValidAndSafe(string([]byte{0xff})) {
		t.Fatal("invalid UTF-8 idempotency key was accepted")
	}
	current := followupTestTask(t, statusRunning)
	path := current.followupQueuePath()
	if err := os.WriteFile(path, []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := current.listFollowups(); err == nil {
		t.Fatal("malformed follow-up queue was accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(current.dir, "queue-target.json")
	if err := os.WriteFile(target, []byte(`{"schema":1,"items":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := current.listFollowups(); err == nil {
		t.Fatal("symlink follow-up queue was accepted")
	}
}

func TestFollowupWaitingApprovalAndAbortDoNotSubstituteOrContinue(t *testing.T) {
	current := followupTestTask(t, statusWaiting)
	current.mu.Lock()
	current.metadata.Approvals = []pendingApproval{{ID: "approval-1", Method: "confirm", Active: true}}
	current.mu.Unlock()
	result, err := current.submitPrompt(promptSubmitParams{TaskID: current.metadata.ID, Prompt: "while approval is pending"})
	if err != nil || result.Disposition != "queued" {
		t.Fatalf("waiting follow-up submit = %+v, err=%v", result, err)
	}
	page, err := current.eventPage(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range page.Events {
		if event.Normalized != nil && event.Normalized.Type == "extension_ui_response" {
			t.Fatal("follow-up submit substituted an approval response")
		}
	}
	if err := current.blockFollowups("task aborted; resume explicitly"); err != nil {
		t.Fatal(err)
	}
	current.mu.Lock()
	current.metadata.Status = statusIdle
	current.mu.Unlock()
	current.dequeueFollowup()
	queue, err := current.readFollowupQueue()
	if err != nil {
		t.Fatal(err)
	}
	if len(queue.Items) != 1 || queue.Items[0].Status != followupBlocked {
		t.Fatalf("aborted task continued follow-up: %+v", queue.Items)
	}
}

func TestQueuedResumeArmsFollowupsAfterWorkerSlotIsReleased(t *testing.T) {
	cfg := testConfig(t)
	manager, err := newTaskManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	current := addSettledSourceTask(t, manager, cfg)
	current.mu.Lock()
	current.metadata.Status = statusRunning
	current.mu.Unlock()
	item, err := current.enqueueFollowup(followupEnqueueParams{TaskID: current.metadata.ID, Prompt: "resume follow-up"})
	if err != nil {
		t.Fatal(err)
	}
	if err := current.blockFollowups("turn failed; resume explicitly"); err != nil {
		t.Fatal(err)
	}
	current.mu.Lock()
	current.metadata.Status = statusStopped
	current.mu.Unlock()

	blocker := &task{manager: manager, metadata: taskMetadata{ID: "ffeeddccbbaa998877665544", Status: statusRunning, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}}
	manager.mu.Lock()
	manager.tasks[blocker.metadata.ID] = blocker
	manager.mu.Unlock()
	if err := current.queue(queuedLaunch{Operation: "resume", SessionFile: current.metadata.SessionFile}); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, current, statusQueued)
	blocker.mu.Lock()
	blocker.metadata.Status = statusIdle
	blocker.mu.Unlock()
	manager.scheduleQueued()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		page, pageErr := current.eventPage(0, 100)
		if pageErr == nil {
			for _, event := range page.Events {
				if bytes.Contains(event.Event, []byte(`"type":"hobot_followup_armed"`)) && bytes.Contains(event.Event, []byte(item.ID)) {
					current.mu.Lock()
					current.metadata.Status = statusIdle
					current.stdin = &followupNopCloser{}
					current.mu.Unlock()
					if err := current.armFollowups(); err != nil {
						t.Fatal(err)
					}
					current.dequeueFollowup()
					queue, readErr := current.readFollowupQueue()
					if readErr == nil && len(queue.Items) == 1 && queue.Items[0].ID == item.ID && queue.Items[0].Status == followupSent {
						return
					}
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	queue, err := current.readFollowupQueue()
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("queued resume did not arm and deliver follow-up: %+v", queue.Items)
}

func TestFollowupDispatchingRecoveryIsUncertainBarrier(t *testing.T) {
	current := followupTestTask(t, statusIdle)
	now := time.Now().UTC()
	first := followupMessage{ID: "aaaaaaaaaaaaaaaaaaaaaaaa", Prompt: "uncertain", Status: followupDispatching, QueuedAt: now, UpdatedAt: now}
	second := followupMessage{ID: "bbbbbbbbbbbbbbbbbbbbbbbb", Prompt: "later", Status: followupQueued, QueuedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)}
	if err := writeFollowupQueue(current.followupQueuePath(), followupQueueFile{Items: []followupMessage{first, second}}); err != nil {
		t.Fatal(err)
	}
	if err := current.recoverFollowups(); err != nil {
		t.Fatal(err)
	}
	queue, err := current.readFollowupQueue()
	if err != nil {
		t.Fatal(err)
	}
	if queue.Items[0].Status != followupBlocked || queue.Items[0].Reason != "daemon restarted during delivery; resume to retry safely" {
		t.Fatalf("recovered uncertain item = %+v", queue.Items[0])
	}
	if err := current.armFollowups(); err != nil {
		t.Fatal(err)
	}
	queue, _ = current.readFollowupQueue()
	if queue.Items[0].Status != followupBlocked {
		t.Fatal("generic recovery armed uncertain dispatch")
	}
	current.mu.Lock()
	current.metadata.Status = statusRunning
	current.mu.Unlock()
	if err := current.retryFollowup(first.ID); err != nil {
		t.Fatal(err)
	}
	queue, _ = current.readFollowupQueue()
	if queue.Items[0].Status != followupQueued {
		t.Fatal("explicit retry did not arm uncertain dispatch")
	}
	current.mu.Lock()
	current.metadata.Status = statusIdle
	current.stdin = &followupNopCloser{}
	current.mu.Unlock()
	current.dequeueFollowup()
	queue, _ = current.readFollowupQueue()
	statuses := map[string]followupStatus{}
	for _, item := range queue.Items {
		statuses[item.ID] = item.Status
	}
	if statuses[first.ID] != followupSent || statuses[second.ID] != followupQueued {
		t.Fatalf("FIFO dequeue changed unexpected items: %+v", queue.Items)
	}
	page, err := current.eventPage(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	foundRetryRecovery := false
	for _, event := range page.Events {
		if bytes.Contains(event.Event, []byte(`"type":"hobot_followup_blocked"`)) && bytes.Contains(event.Event, []byte(`"recovery":"retry"`)) {
			foundRetryRecovery = true
		}
	}
	if !foundRetryRecovery {
		t.Fatal("uncertain follow-up block did not advertise retry recovery")
	}
}

func TestFollowupArmRequiresSuccessfulRewriteWhenPaused(t *testing.T) {
	current := followupTestTask(t, statusStopped)
	now := time.Now().UTC()
	uncertain := followupMessage{ID: "aaaaaaaaaaaaaaaaaaaaaaaa", Prompt: "uncertain", Status: followupBlocked, QueuedAt: now, UpdatedAt: now, Reason: "daemon restarted during delivery; resume to retry safely"}
	if err := writeFollowupQueue(current.followupQueuePath(), followupQueueFile{Items: []followupMessage{uncertain}}); err != nil {
		t.Fatal(err)
	}
	current.mu.Lock()
	current.followupFault = true
	current.metadata.FollowupQueuePaused = true
	current.mu.Unlock()
	if err := os.WriteFile(current.followupQueuePath(), []byte("not-json\n"), 0o600); err == nil {
		if err := current.armFollowups(); err == nil {
			t.Fatal("malformed paused queue was accepted during recovery")
		}
	} else {
		t.Fatal(err)
	}
	current.mu.Lock()
	if !current.followupFault || !current.metadata.FollowupQueuePaused {
		current.mu.Unlock()
		t.Fatal("failed paused recovery cleared the fault")
	}
	current.mu.Unlock()
	if err := writeFollowupQueue(current.followupQueuePath(), followupQueueFile{Items: []followupMessage{uncertain}}); err != nil {
		t.Fatal(err)
	}
	if err := current.armFollowups(); err != nil {
		t.Fatal(err)
	}
	current.mu.Lock()
	paused := current.followupFault || current.metadata.FollowupQueuePaused
	current.mu.Unlock()
	if paused {
		t.Fatal("successful paused recovery remained faulted")
	}
	queue, err := current.readFollowupQueue()
	if err != nil || len(queue.Items) != 1 || queue.Items[0].Status != followupBlocked {
		t.Fatalf("uncertain item changed during ordinary resume: %+v err=%v", queue.Items, err)
	}
}

func TestFollowupSubmitChoosesIdleSendOrBusyQueueUnderOneLock(t *testing.T) {
	current := followupTestTask(t, statusIdle)
	current.mu.Lock()
	current.stdin = &followupNopCloser{}
	current.mu.Unlock()
	result, err := current.submitPrompt(promptSubmitParams{TaskID: current.metadata.ID, Prompt: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Disposition != "sent" {
		t.Fatalf("idle disposition = %q", result.Disposition)
	}
	current.mu.Lock()
	current.metadata.Status = statusRunning
	current.mu.Unlock()
	queued, err := current.submitPrompt(promptSubmitParams{TaskID: current.metadata.ID, Prompt: "queued"})
	if err != nil {
		t.Fatal(err)
	}
	if queued.Disposition != "queued" || queued.Item == nil {
		t.Fatalf("busy disposition = %+v", queued)
	}
	content, err := os.ReadFile(current.events)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(content, []byte(`"message":"direct"`)) || !bytes.Contains(content, []byte(`"type":"hobot_followup_queued"`)) {
		t.Fatalf("submit events missing: %s", content)
	}
	var check map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(bytes.Split(content, []byte{'\n'})[0]), &check); err != nil && err != io.EOF {
		t.Fatal(err)
	}
}

func TestFollowupPromptSubmitIsIdempotentAcrossDirectAndQueuedStates(t *testing.T) {
	current := followupTestTask(t, statusIdle)
	current.mu.Lock()
	current.stdin = &followupNopCloser{}
	current.mu.Unlock()
	first, err := current.submitPrompt(promptSubmitParams{TaskID: current.metadata.ID, Prompt: "direct once", IdempotencyKey: "direct-key"})
	if err != nil || first.Disposition != "sent" {
		t.Fatalf("initial direct submit = %+v, err=%v", first, err)
	}
	before, err := current.eventPage(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	current.mu.Lock()
	current.metadata.Status = statusRunning
	current.mu.Unlock()
	retry, err := current.submitPrompt(promptSubmitParams{TaskID: current.metadata.ID, Prompt: "direct once", IdempotencyKey: "direct-key"})
	if err != nil || retry.Disposition != "sent" || retry.Uncertain {
		t.Fatalf("direct response retry = %+v, err=%v", retry, err)
	}
	after, err := current.eventPage(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Events) != len(before.Events) {
		t.Fatalf("direct retry wrote duplicate events: before=%d after=%d", len(before.Events), len(after.Events))
	}
	if _, err := current.submitPrompt(promptSubmitParams{TaskID: current.metadata.ID, Prompt: "different", IdempotencyKey: "direct-key"}); err == nil {
		t.Fatal("direct key was reused for a different prompt")
	}
	queue, err := current.readFollowupQueue()
	if err != nil {
		t.Fatal(err)
	}
	queue.DirectSubmits = append(queue.DirectSubmits, directSubmitReceipt{IdempotencyKey: "uncertain-key", Fingerprint: directPromptFingerprint("uncertain direct", nil), Status: directSubmitDispatching, UpdatedAt: time.Now().UTC()})
	if err := writeFollowupQueue(current.followupQueuePath(), queue); err != nil {
		t.Fatal(err)
	}
	uncertain, err := current.submitPrompt(promptSubmitParams{TaskID: current.metadata.ID, Prompt: "uncertain direct", IdempotencyKey: "uncertain-key"})
	if err != nil || uncertain.Disposition != "sent" || !uncertain.Uncertain || uncertain.Item != nil {
		t.Fatalf("uncertain direct retry = %+v, err=%v", uncertain, err)
	}

	queued, err := current.submitPrompt(promptSubmitParams{TaskID: current.metadata.ID, Prompt: "queued once", IdempotencyKey: "queued-key"})
	if err != nil || queued.Disposition != "queued" || queued.Item == nil {
		t.Fatalf("initial queued submit = %+v, err=%v", queued, err)
	}
	if err := current.cancelFollowup(queued.Item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := current.submitPrompt(promptSubmitParams{TaskID: current.metadata.ID, Prompt: "queued once", IdempotencyKey: "queued-key"}); err == nil {
		t.Fatal("cancelled queue key was silently reused")
	}
}

func TestFollowupQueuedReceiptRetryAfterDeliveryDoesNotDirectSend(t *testing.T) {
	current := followupTestTask(t, statusRunning)
	queued, err := current.submitPrompt(promptSubmitParams{TaskID: current.metadata.ID, Prompt: "queued delivery", IdempotencyKey: "queued-delivery-key"})
	if err != nil || queued.Item == nil {
		t.Fatalf("queue submit = %+v, err=%v", queued, err)
	}
	queue, err := current.readFollowupQueue()
	if err != nil {
		t.Fatal(err)
	}
	for index := range queue.Items {
		if queue.Items[index].ID == queued.Item.ID {
			queue.Items[index].Status = followupSent
		}
	}
	if err := writeFollowupQueue(current.followupQueuePath(), queue); err != nil {
		t.Fatal(err)
	}
	current.mu.Lock()
	current.metadata.Status = statusIdle
	current.stdin = &followupNopCloser{}
	current.mu.Unlock()
	result, err := current.submitPrompt(promptSubmitParams{TaskID: current.metadata.ID, Prompt: "queued delivery", IdempotencyKey: "queued-delivery-key"})
	if err != nil || result.Disposition != "sent" {
		t.Fatalf("queued delivery retry = %+v, err=%v", result, err)
	}
	content, err := os.ReadFile(current.events)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(content, []byte(`"message":"queued delivery"`)) != 0 {
		t.Fatal("retry after queued delivery sent a duplicate direct prompt")
	}
}

func TestFollowupFinalModelErrorBlocksAfterSettled(t *testing.T) {
	current := followupTestTask(t, statusRunning)
	item, err := current.enqueueFollowup(followupEnqueueParams{TaskID: current.metadata.ID, Prompt: "after failure"})
	if err != nil {
		t.Fatal(err)
	}
	current.recordEvent(json.RawMessage(`{"type":"agent_start"}`))
	current.recordEvent(json.RawMessage(`{"type":"message_end","message":{"role":"assistant","errorMessage":"model unavailable"}}`))
	current.recordEvent(json.RawMessage(`{"type":"agent_settled"}`))
	time.Sleep(30 * time.Millisecond)
	queue, err := current.readFollowupQueue()
	if err != nil {
		t.Fatal(err)
	}
	if len(queue.Items) != 1 || queue.Items[0].ID != item.ID || queue.Items[0].Status != followupBlocked {
		t.Fatalf("failed turn continued follow-up: %+v", queue.Items)
	}
}

func TestFollowupRetrySuccessClearsPreviousTurnFailure(t *testing.T) {
	current := followupTestTask(t, statusRunning)
	current.recordEvent(json.RawMessage(`{"type":"agent_start"}`))
	current.recordEvent(json.RawMessage(`{"type":"message_end","message":{"role":"assistant","errorMessage":"temporary outage"}}`))
	current.recordEvent(json.RawMessage(`{"type":"agent_start"}`))
	current.recordEvent(json.RawMessage(`{"type":"message_end","message":{"role":"assistant","stopReason":"stop"}}`))
	current.mu.Lock()
	failed := current.turnFailed
	current.mu.Unlock()
	if failed {
		t.Fatal("successful retry retained the previous turn failure")
	}
}
