package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"
	"unicode/utf8"
)

const (
	followupQueueSchema        = 1
	maximumFollowupItems       = 10
	maximumFollowupRecords     = 64
	maximumFollowupText        = 256 * 1024
	maximumFollowupQueueBytes  = 8 * maxRequestBytes
	maximumFollowupIdempotency = 128
)

var followupIDPattern = regexp.MustCompile(`^[0-9a-f]{24}$`)

type followupStatus string

const (
	followupQueued      followupStatus = "queued"
	followupDispatching followupStatus = "dispatching"
	followupSent        followupStatus = "sent"
	followupCancelled   followupStatus = "cancelled"
	followupBlocked     followupStatus = "blocked"
)

type followupMessage struct {
	ID             string         `json:"id"`
	Prompt         string         `json:"prompt"`
	Images         []imageContent `json:"images,omitempty"`
	Status         followupStatus `json:"status"`
	IdempotencyKey string         `json:"idempotencyKey,omitempty"`
	Fingerprint    string         `json:"fingerprint,omitempty"`
	QueuedAt       time.Time      `json:"queuedAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
	Reason         string         `json:"reason,omitempty"`
}

type followupQueueFile struct {
	Schema        int                   `json:"schema"`
	Items         []followupMessage     `json:"items"`
	DirectSubmits []directSubmitReceipt `json:"directSubmits,omitempty"`
}

type directSubmitReceipt struct {
	IdempotencyKey string    `json:"idempotencyKey"`
	Fingerprint    string    `json:"fingerprint"`
	Status         string    `json:"status"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

const (
	directSubmitDispatching = "dispatching"
	directSubmitSent        = "sent"
	directSubmitUncertain   = "uncertain"
)

type followupEnqueueParams struct {
	TaskID         string         `json:"taskId"`
	Prompt         string         `json:"prompt"`
	Images         []imageContent `json:"images,omitempty"`
	IdempotencyKey string         `json:"idempotencyKey,omitempty"`
}

type followupCancelParams struct {
	TaskID  string `json:"taskId"`
	QueueID string `json:"queueId"`
}

type followupQueueParams struct {
	TaskID string `json:"taskId"`
}

type followupRetryParams struct {
	TaskID  string `json:"taskId"`
	QueueID string `json:"queueId"`
}

type promptSubmitParams struct {
	TaskID         string         `json:"taskId"`
	Prompt         string         `json:"prompt"`
	Images         []imageContent `json:"images,omitempty"`
	IdempotencyKey string         `json:"idempotencyKey,omitempty"`
}

type promptSubmitResult struct {
	Disposition string               `json:"disposition"`
	Item        *followupMessageView `json:"item,omitempty"`
	Uncertain   bool                 `json:"uncertain,omitempty"`
}

type followupMessageView struct {
	ID             string              `json:"id"`
	Prompt         string              `json:"prompt"`
	Status         followupStatus      `json:"status"`
	IdempotencyKey string              `json:"idempotencyKey,omitempty"`
	QueuedAt       time.Time           `json:"queuedAt"`
	UpdatedAt      time.Time           `json:"updatedAt"`
	Reason         string              `json:"reason,omitempty"`
	Recovery       string              `json:"recovery,omitempty"`
	Attachments    []map[string]string `json:"attachments,omitempty"`
}

type followupQueueResult struct {
	Items []followupMessageView `json:"items"`
}

func (current *task) followupQueuePath() string {
	return filepath.Join(current.dir, "followup-queue.json")
}

func (current *task) markFollowupFault(err error) {
	current.mu.Lock()
	current.followupFault = true
	current.metadata.FollowupQueuePaused = true
	current.metadata.UpdatedAt = time.Now().UTC()
	current.mu.Unlock()
	_ = current.saveMetadata()
	if err != nil {
		current.appendFailureDetail("follow-up queue paused: " + err.Error())
	}
}

func followupPayloadBytes(prompt string, images []imageContent) int {
	return len(prompt)
}

func validateFollowupInput(prompt string, images []imageContent, key string) error {
	if prompt == "" || len(prompt) > maxPromptBytes {
		return fmt.Errorf("follow-up prompt must contain 1 to %d bytes", maxPromptBytes)
	}
	if err := validateImages(images); err != nil {
		return err
	}
	if len(prompt) > maximumFollowupText {
		return fmt.Errorf("follow-up text exceeds the %d byte limit", maximumFollowupText)
	}
	if len(key) > maximumFollowupIdempotency || (key != "" && (!utf8.ValidString(key) || !utf8ValidAndSafe(key))) {
		return fmt.Errorf("follow-up idempotency key must contain at most %d safe characters", maximumFollowupIdempotency)
	}
	return nil
}

func utf8ValidAndSafe(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func (current *task) readFollowupQueue() (followupQueueFile, error) {
	content, err := readPrivateRegularFile(current.followupQueuePath(), maximumFollowupQueueBytes)
	if errors.Is(err, os.ErrNotExist) {
		return followupQueueFile{Schema: followupQueueSchema, Items: []followupMessage{}}, nil
	}
	if err != nil {
		return followupQueueFile{}, err
	}
	var queue followupQueueFile
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&queue); err != nil {
		return followupQueueFile{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return followupQueueFile{}, fmt.Errorf("follow-up queue must contain exactly one JSON object")
	}
	if queue.Schema != followupQueueSchema || len(queue.Items) > maximumFollowupRecords {
		return followupQueueFile{}, fmt.Errorf("follow-up queue metadata is invalid")
	}
	if len(queue.DirectSubmits) > maximumFollowupRecords {
		return followupQueueFile{}, fmt.Errorf("follow-up direct receipt metadata is invalid")
	}
	for index := range queue.Items {
		item := &queue.Items[index]
		if !followupIDPattern.MatchString(item.ID) || item.QueuedAt.IsZero() || item.UpdatedAt.IsZero() ||
			(item.Status != followupQueued && item.Status != followupDispatching && item.Status != followupSent && item.Status != followupCancelled && item.Status != followupBlocked) {
			return followupQueueFile{}, fmt.Errorf("follow-up queue item %d is invalid", index+1)
		}
		if item.Fingerprint != "" && (len(item.Fingerprint) != sha256.Size*2 || !isLowerHex(item.Fingerprint, sha256.Size*2)) {
			return followupQueueFile{}, fmt.Errorf("follow-up queue item %d fingerprint is invalid", index+1)
		}
		if len(item.IdempotencyKey) > maximumFollowupIdempotency || (item.IdempotencyKey != "" && !utf8ValidAndSafe(item.IdempotencyKey)) {
			return followupQueueFile{}, fmt.Errorf("follow-up queue item %d idempotency key is invalid", index+1)
		}
		if item.Status == followupQueued || item.Status == followupDispatching || item.Status == followupBlocked {
			if err := validateFollowupInput(item.Prompt, item.Images, item.IdempotencyKey); err != nil {
				return followupQueueFile{}, fmt.Errorf("follow-up queue item %s: %w", item.ID, err)
			}
		}
	}
	for index, receipt := range queue.DirectSubmits {
		if len(receipt.IdempotencyKey) == 0 || len(receipt.IdempotencyKey) > maximumFollowupIdempotency || !utf8ValidAndSafe(receipt.IdempotencyKey) ||
			len(receipt.Fingerprint) != sha256.Size*2 || !isLowerHex(receipt.Fingerprint, sha256.Size*2) || receipt.UpdatedAt.IsZero() ||
			(receipt.Status != directSubmitDispatching && receipt.Status != directSubmitSent && receipt.Status != directSubmitUncertain) {
			return followupQueueFile{}, fmt.Errorf("follow-up direct receipt %d is invalid", index+1)
		}
	}
	return queue, nil
}

func writeFollowupQueue(path string, queue followupQueueFile) error {
	queue.Schema = followupQueueSchema
	queue.Items = compactFollowupItems(queue.Items)
	queue.DirectSubmits = compactDirectSubmitReceipts(queue.DirectSubmits)
	encoded, err := json.MarshalIndent(queue, "", "  ")
	if err != nil {
		return err
	}
	if len(encoded)+1 > maximumFollowupQueueBytes {
		return fmt.Errorf("follow-up queue storage exceeds the %d byte limit", maximumFollowupQueueBytes)
	}
	return writePrivateFile(path, append(encoded, '\n'))
}

func followupPending(item followupMessage) bool {
	return item.Status == followupQueued || item.Status == followupDispatching || item.Status == followupBlocked
}

func followupViews(items []followupMessage) followupQueueResult {
	result := followupQueueResult{Items: make([]followupMessageView, 0, len(items))}
	for _, item := range items {
		if item.Status == followupSent || item.Status == followupCancelled {
			continue
		}
		result.Items = append(result.Items, followupView(item))
	}
	return result
}

func followupView(item followupMessage) followupMessageView {
	recovery := ""
	if item.Status == followupBlocked {
		recovery = "resume"
		if item.Reason == "daemon restarted during delivery; resume to retry safely" {
			recovery = "retry"
		}
	}
	view := followupMessageView{ID: item.ID, Prompt: item.Prompt, Status: item.Status, IdempotencyKey: item.IdempotencyKey, QueuedAt: item.QueuedAt, UpdatedAt: item.UpdatedAt, Reason: item.Reason, Recovery: recovery}
	for _, image := range item.Images {
		view.Attachments = append(view.Attachments, map[string]string{"name": image.Name, "mimeType": image.MimeType})
	}
	return view
}

func (current *task) enqueueFollowup(params followupEnqueueParams) (followupMessageView, error) {
	current.followupMu.Lock()
	defer current.followupMu.Unlock()
	return current.enqueueFollowupLocked(params)
}

func (current *task) enqueueFollowupLocked(params followupEnqueueParams) (followupMessageView, error) {
	if err := validateFollowupInput(params.Prompt, params.Images, params.IdempotencyKey); err != nil {
		return followupMessageView{}, err
	}
	current.mu.Lock()
	status := current.metadata.Status
	archived := current.metadata.ArchivedAt != nil
	current.mu.Unlock()
	if archived {
		return followupMessageView{}, fmt.Errorf("unarchive the task before queueing a follow-up")
	}
	if status != statusRunning && status != statusWaiting && status != statusStarting {
		if status == statusIdle {
			return followupMessageView{}, fmt.Errorf("task is idle; send directly instead of queueing a follow-up")
		}
		return followupMessageView{}, fmt.Errorf("follow-up queue accepts messages only while the task is working or waiting for approval")
	}
	queue, err := current.readFollowupQueue()
	if err != nil {
		return followupMessageView{}, fmt.Errorf("read follow-up queue: %w", err)
	}
	if params.IdempotencyKey != "" {
		for _, item := range queue.Items {
			if item.IdempotencyKey == params.IdempotencyKey {
				fingerprint := directPromptFingerprint(params.Prompt, params.Images)
				if (item.Fingerprint != "" && item.Fingerprint != fingerprint) || (item.Fingerprint == "" && item.Prompt != "" && directPromptFingerprint(item.Prompt, item.Images) != fingerprint) {
					return followupMessageView{}, fmt.Errorf("idempotency key was already used for a different follow-up")
				}
				if item.Status == followupCancelled {
					return followupMessageView{}, fmt.Errorf("follow-up idempotency key was cancelled; submit with a new key")
				}
				return followupView(item), nil
			}
		}
	}
	pending := 0
	payloadBytes := 0
	for _, item := range queue.Items {
		if followupPending(item) {
			pending++
			payloadBytes += followupPayloadBytes(item.Prompt, item.Images)
		}
	}
	if pending >= maximumFollowupItems {
		return followupMessageView{}, fmt.Errorf("follow-up queue is full; at most %d messages may be pending", maximumFollowupItems)
	}
	if payloadBytes+followupPayloadBytes(params.Prompt, params.Images) > maximumFollowupText {
		return followupMessageView{}, fmt.Errorf("follow-up queue text exceeds the %d byte limit", maximumFollowupText)
	}
	id, err := newTaskID()
	if err != nil {
		return followupMessageView{}, err
	}
	now := time.Now().UTC()
	item := followupMessage{ID: id, Prompt: params.Prompt, Images: append([]imageContent(nil), params.Images...), Status: followupQueued, IdempotencyKey: params.IdempotencyKey, Fingerprint: directPromptFingerprint(params.Prompt, params.Images), QueuedAt: now, UpdatedAt: now}
	queue.Items = append(queue.Items, item)
	if err := writeFollowupQueue(current.followupQueuePath(), queue); err != nil {
		return followupMessageView{}, fmt.Errorf("persist follow-up queue: %w", err)
	}
	current.recordFollowupEvent("hobot_followup_queued", item, "")
	return followupView(item), nil
}

func (current *task) listFollowups() (followupQueueResult, error) {
	current.followupMu.Lock()
	defer current.followupMu.Unlock()
	queue, err := current.readFollowupQueue()
	if err != nil {
		return followupQueueResult{}, err
	}
	return followupViews(queue.Items), nil
}

func (current *task) submitPrompt(params promptSubmitParams) (promptSubmitResult, error) {
	if err := validateFollowupInput(params.Prompt, params.Images, params.IdempotencyKey); err != nil {
		return promptSubmitResult{}, err
	}
	current.followupMu.Lock()
	defer current.followupMu.Unlock()
	current.mu.Lock()
	status := current.metadata.Status
	current.mu.Unlock()
	queue, err := current.readFollowupQueue()
	if err != nil {
		current.markFollowupFault(err)
		return promptSubmitResult{}, fmt.Errorf("read follow-up receipts: %w", err)
	}
	fingerprint := directPromptFingerprint(params.Prompt, params.Images)
	if params.IdempotencyKey != "" {
		for _, item := range queue.Items {
			if item.IdempotencyKey != params.IdempotencyKey {
				continue
			}
			if (item.Fingerprint != "" && item.Fingerprint != fingerprint) || (item.Fingerprint == "" && item.Prompt != "" && directPromptFingerprint(item.Prompt, item.Images) != fingerprint) {
				return promptSubmitResult{}, fmt.Errorf("idempotency key was already used for a different prompt")
			}
			switch item.Status {
			case followupSent:
				return promptSubmitResult{Disposition: "sent"}, nil
			case followupCancelled:
				return promptSubmitResult{}, fmt.Errorf("follow-up idempotency key was cancelled; submit with a new key")
			default:
				view := followupView(item)
				return promptSubmitResult{Disposition: "queued", Item: &view}, nil
			}
		}
		for _, receipt := range queue.DirectSubmits {
			if receipt.IdempotencyKey != params.IdempotencyKey {
				continue
			}
			if receipt.Fingerprint != fingerprint {
				return promptSubmitResult{}, fmt.Errorf("idempotency key was already used for a different prompt")
			}
			if receipt.Status == directSubmitSent || receipt.Status == directSubmitDispatching || receipt.Status == directSubmitUncertain {
				return promptSubmitResult{Disposition: "sent", Uncertain: receipt.Status != directSubmitSent}, nil
			}
		}
	}
	if status == statusIdle {
		receiptIndex := -1
		if params.IdempotencyKey != "" {
			now := time.Now().UTC()
			queue.DirectSubmits = append(queue.DirectSubmits, directSubmitReceipt{IdempotencyKey: params.IdempotencyKey, Fingerprint: fingerprint, Status: directSubmitDispatching, UpdatedAt: now})
			receiptIndex = len(queue.DirectSubmits) - 1
			if err := writeFollowupQueue(current.followupQueuePath(), queue); err != nil {
				current.markFollowupFault(err)
				return promptSubmitResult{}, fmt.Errorf("persist direct prompt receipt: %w", err)
			}
		}
		command, _ := json.Marshal(map[string]any{"id": "prompt-" + time.Now().UTC().Format("20060102150405.000000000"), "type": "prompt", "message": params.Prompt, "images": params.Images})
		if err := current.sendCommandWithPromptEvent(command, true); err != nil {
			if receiptIndex >= 0 {
				queue.DirectSubmits[receiptIndex].Status = directSubmitUncertain
				queue.DirectSubmits[receiptIndex].UpdatedAt = time.Now().UTC()
				if persistErr := writeFollowupQueue(current.followupQueuePath(), queue); persistErr != nil {
					current.markFollowupFault(persistErr)
				}
			}
			return promptSubmitResult{}, err
		}
		if receiptIndex >= 0 {
			queue.DirectSubmits[receiptIndex].Status = directSubmitSent
			queue.DirectSubmits[receiptIndex].UpdatedAt = time.Now().UTC()
			if err := writeFollowupQueue(current.followupQueuePath(), queue); err != nil {
				current.markFollowupFault(err)
				return promptSubmitResult{Disposition: "sent", Uncertain: true}, fmt.Errorf("persist direct prompt receipt: %w", err)
			}
		}
		return promptSubmitResult{Disposition: "sent"}, nil
	}
	if status != statusRunning && status != statusWaiting && status != statusStarting {
		return promptSubmitResult{}, fmt.Errorf("task is not accepting a prompt while in %s", status)
	}
	item, err := current.enqueueFollowupLocked(followupEnqueueParams{TaskID: params.TaskID, Prompt: params.Prompt, Images: params.Images, IdempotencyKey: params.IdempotencyKey})
	if err != nil {
		return promptSubmitResult{}, err
	}
	if item.Status == followupSent {
		return promptSubmitResult{Disposition: "sent", Item: &item}, nil
	}
	return promptSubmitResult{Disposition: "queued", Item: &item}, nil
}

func (current *task) cancelFollowup(queueID string) error {
	if !followupIDPattern.MatchString(queueID) {
		return fmt.Errorf("queueId is invalid")
	}
	current.followupMu.Lock()
	defer current.followupMu.Unlock()
	queue, err := current.readFollowupQueue()
	if err != nil {
		return err
	}
	for index := range queue.Items {
		item := &queue.Items[index]
		if item.ID != queueID {
			continue
		}
		if item.Status != followupQueued && item.Status != followupBlocked {
			return fmt.Errorf("follow-up %s is already %s and cannot be cancelled", queueID, item.Status)
		}
		item.Status = followupCancelled
		item.UpdatedAt = time.Now().UTC()
		item.Reason = "cancelled by user"
		cancelled := *item
		queue.Items = compactFollowupItems(queue.Items)
		if err := writeFollowupQueue(current.followupQueuePath(), queue); err != nil {
			return err
		}
		current.recordFollowupEvent("hobot_followup_cancelled", cancelled, cancelled.Reason)
		return nil
	}
	return fmt.Errorf("follow-up does not exist: %s", queueID)
}

func (current *task) recordFollowupEvent(kind string, item followupMessage, reason string) {
	event := map[string]any{"type": kind, "queueId": item.ID, "queuedAt": item.QueuedAt, "status": item.Status}
	if reason != "" {
		event["reason"] = reason
	}
	if item.Status == followupBlocked {
		event["recovery"] = "resume"
		if item.Reason == "daemon restarted during delivery; resume to retry safely" {
			event["recovery"] = "retry"
		}
	}
	raw, _ := json.Marshal(event)
	current.recordEvent(raw)
}

func (current *task) dequeueFollowup() {
	current.followupMu.Lock()
	defer current.followupMu.Unlock()
	current.mu.Lock()
	if current.metadata.Status != statusIdle || current.metadata.FollowupQueuePaused || current.followupFault {
		current.mu.Unlock()
		return
	}
	current.mu.Unlock()
	queue, err := current.readFollowupQueue()
	if err != nil {
		current.markFollowupFault(err)
		return
	}
	index := -1
	for candidate := range queue.Items {
		if queue.Items[candidate].Status == followupDispatching {
			return
		}
		if queue.Items[candidate].Status == followupQueued {
			index = candidate
			break
		}
	}
	if index < 0 {
		return
	}
	item := &queue.Items[index]
	item.Status = followupDispatching
	item.UpdatedAt = time.Now().UTC()
	if err := writeFollowupQueue(current.followupQueuePath(), queue); err != nil {
		current.markFollowupFault(err)
		return
	}
	current.recordFollowupEvent("hobot_followup_dispatching", *item, "")
	command, _ := json.Marshal(map[string]any{"id": "followup-" + item.ID, "type": "prompt", "message": item.Prompt, "images": item.Images})
	if err := current.sendCommandWithPromptEvent(command, true, promptEventOrigin{QueueID: item.ID}); err != nil {
		item.Status = followupBlocked
		item.UpdatedAt = time.Now().UTC()
		item.Reason = "delivery failed; resume to retry safely"
		if writeErr := writeFollowupQueue(current.followupQueuePath(), queue); writeErr != nil {
			current.markFollowupFault(writeErr)
			return
		}
		current.recordFollowupEvent("hobot_followup_blocked", *item, item.Reason)
		return
	}
	item.Status = followupSent
	item.UpdatedAt = time.Now().UTC()
	item.Reason = "delivered to worker"
	if err := writeFollowupQueue(current.followupQueuePath(), queue); err != nil {
		current.markFollowupFault(err)
		return
	}
	current.recordFollowupEvent("hobot_followup_sent", *item, "")
}

func (current *task) blockFollowups(reason string) error {
	current.followupMu.Lock()
	defer current.followupMu.Unlock()
	queue, err := current.readFollowupQueue()
	if err != nil {
		current.markFollowupFault(err)
		return err
	}
	changed := false
	for index := range queue.Items {
		item := &queue.Items[index]
		if item.Status != followupQueued && item.Status != followupDispatching {
			continue
		}
		item.Status = followupBlocked
		item.UpdatedAt = time.Now().UTC()
		item.Reason = reason
		changed = true
	}
	if changed {
		if err := writeFollowupQueue(current.followupQueuePath(), queue); err != nil {
			current.markFollowupFault(err)
			return err
		}
	}
	if changed {
		for _, item := range queue.Items {
			if item.Status == followupBlocked && item.Reason == reason {
				current.recordFollowupEvent("hobot_followup_blocked", item, reason)
			}
		}
	}
	return nil
}

func (current *task) armFollowups() error {
	current.followupMu.Lock()
	defer current.followupMu.Unlock()
	queue, err := current.readFollowupQueue()
	if err != nil {
		return err
	}
	current.mu.Lock()
	paused := current.metadata.FollowupQueuePaused || current.followupFault
	current.mu.Unlock()
	changed := false
	for index := range queue.Items {
		item := &queue.Items[index]
		if item.Status != followupBlocked || item.Reason == "daemon restarted during delivery; resume to retry safely" {
			continue
		}
		item.Status = followupQueued
		item.UpdatedAt = time.Now().UTC()
		item.Reason = "armed after explicit recovery"
		changed = true
	}
	if paused || changed {
		if err := writeFollowupQueue(current.followupQueuePath(), queue); err != nil {
			current.markFollowupFault(err)
			return err
		}
	}
	if changed {
		for _, item := range queue.Items {
			if item.Status == followupQueued && item.Reason == "armed after explicit recovery" {
				current.recordFollowupEvent("hobot_followup_armed", item, "")
			}
		}
	}
	if paused {
		current.mu.Lock()
		current.followupFault = false
		current.metadata.FollowupQueuePaused = false
		current.mu.Unlock()
		if err := current.saveMetadata(); err != nil {
			current.markFollowupFault(err)
			return err
		}
	}
	return nil
}

func (current *task) recoverFollowups() error {
	current.followupMu.Lock()
	defer current.followupMu.Unlock()
	queue, err := current.readFollowupQueue()
	if err != nil {
		return err
	}
	changed := false
	for index := range queue.Items {
		item := &queue.Items[index]
		if item.Status != followupDispatching {
			continue
		}
		item.Status = followupBlocked
		item.UpdatedAt = time.Now().UTC()
		item.Reason = "daemon restarted during delivery; resume to retry safely"
		changed = true
	}
	if !changed {
		return nil
	}
	if err := writeFollowupQueue(current.followupQueuePath(), queue); err != nil {
		current.markFollowupFault(err)
		return err
	}
	for _, item := range queue.Items {
		if item.Status == followupBlocked && item.Reason == "daemon restarted during delivery; resume to retry safely" {
			current.recordFollowupEvent("hobot_followup_blocked", item, item.Reason)
		}
	}
	return nil
}

func (current *task) resumeFollowups() error {
	if err := current.armFollowups(); err != nil {
		return err
	}
	current.mu.Lock()
	status := current.metadata.Status
	current.mu.Unlock()
	if status == statusIdle {
		go current.dequeueFollowup()
	}
	return nil
}

func (current *task) retryFollowup(queueID string) error {
	if !followupIDPattern.MatchString(queueID) {
		return fmt.Errorf("queueId is invalid")
	}
	current.followupMu.Lock()
	defer current.followupMu.Unlock()
	queue, err := current.readFollowupQueue()
	if err != nil {
		current.markFollowupFault(err)
		return err
	}
	for index := range queue.Items {
		item := &queue.Items[index]
		if item.ID != queueID {
			continue
		}
		if item.Status != followupBlocked || item.Reason != "daemon restarted during delivery; resume to retry safely" {
			return fmt.Errorf("follow-up %s is not awaiting explicit retry", queueID)
		}
		item.Status = followupQueued
		item.UpdatedAt = time.Now().UTC()
		item.Reason = "explicitly retried after uncertain delivery"
		copy := *item
		if err := writeFollowupQueue(current.followupQueuePath(), queue); err != nil {
			current.markFollowupFault(err)
			return err
		}
		current.recordFollowupEvent("hobot_followup_armed", copy, "")
		current.mu.Lock()
		idle := current.metadata.Status == statusIdle && !current.metadata.FollowupQueuePaused && !current.followupFault
		current.mu.Unlock()
		if idle {
			go current.dequeueFollowup()
		}
		return nil
	}
	return fmt.Errorf("follow-up does not exist: %s", queueID)
}

func compactFollowupItems(items []followupMessage) []followupMessage {
	result := make([]followupMessage, 0, len(items))
	terminal := make([]followupMessage, 0)
	for _, item := range items {
		if item.Status == followupSent || item.Status == followupCancelled {
			item.Prompt = ""
			item.Images = nil
			terminal = append(terminal, item)
			continue
		}
		result = append(result, item)
	}
	if len(terminal) > maximumFollowupRecords-len(result) {
		terminal = terminal[len(terminal)-(maximumFollowupRecords-len(result)):]
	}
	return append(result, terminal...)
}

func compactDirectSubmitReceipts(receipts []directSubmitReceipt) []directSubmitReceipt {
	if len(receipts) <= maximumFollowupRecords {
		return receipts
	}
	return receipts[len(receipts)-maximumFollowupRecords:]
}

func directPromptFingerprint(prompt string, images []imageContent) string {
	payload, _ := json.Marshal(struct {
		Prompt string         `json:"prompt"`
		Images []imageContent `json:"images,omitempty"`
	}{Prompt: prompt, Images: images})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}
