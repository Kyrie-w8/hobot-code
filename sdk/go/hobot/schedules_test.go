package hobot

import (
	"testing"
	"time"
)

func validSDKSchedule() Schedule {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	next := now.Add(time.Minute)
	return Schedule{
		ID: "0123456789abcdef01234567", Name: "health", TaskID: "abcdef0123456789abcdef01",
		Every: "1m0s", Enabled: true, Status: "active", CreatedAt: now, UpdatedAt: now, NextRun: &next,
	}
}

func TestValidateScheduleRejectsMalformedBoardData(t *testing.T) {
	valid := validSDKSchedule()
	if err := validateSchedule(valid); err != nil {
		t.Fatalf("valid schedule rejected: %v", err)
	}
	tests := []struct {
		name string
		edit func(*Schedule)
	}{
		{"identity", func(value *Schedule) { value.ID = "../escape" }},
		{"cadence", func(value *Schedule) { at := value.CreatedAt.Add(time.Hour); value.At = &at }},
		{"interval", func(value *Schedule) { value.Every = "1s" }},
		{"status", func(value *Schedule) { value.Status = "unknown" }},
		{"active-without-next", func(value *Schedule) { value.NextRun = nil }},
		{"pending-paused", func(value *Schedule) { value.Status = "paused"; value.Enabled = false; value.Pending = true }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := valid
			test.edit(&value)
			if err := validateSchedule(value); err == nil {
				t.Fatalf("malformed schedule accepted: %+v", value)
			}
		})
	}
}

func TestValidateSchedulesRejectsDuplicatesAndExcess(t *testing.T) {
	valid := validSDKSchedule()
	if err := validateSchedules([]Schedule{valid, valid}); err == nil {
		t.Fatal("duplicate schedule IDs were accepted")
	}
	values := make([]Schedule, maximumSDKScheduleCount+1)
	if err := validateSchedules(values); err == nil {
		t.Fatal("excessive schedule list was accepted")
	}
}

func TestCreateScheduleRequestRequiresOneCadence(t *testing.T) {
	base := CreateScheduleRequest{TaskID: "abcdef0123456789abcdef01", Prompt: "check"}
	if err := validateCreateScheduleRequest(base); err == nil {
		t.Fatal("schedule without cadence was accepted")
	}
	base.At = "2026-08-18T09:00:00+08:00"
	base.Every = "1m"
	if err := validateCreateScheduleRequest(base); err == nil {
		t.Fatal("schedule with two cadences was accepted")
	}
	base.At = ""
	if err := validateCreateScheduleRequest(base); err != nil {
		t.Fatalf("valid recurring request rejected: %v", err)
	}
}
