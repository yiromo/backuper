package config

import (
	"testing"
	"time"
)

func TestScheduleConfig_ScheduleType(t *testing.T) {
	tests := []struct {
		name     string
		cron     string
		expected ScheduleType
	}{
		{"daily", "0 3 * * *", ScheduleTypeDaily},
		{"daily different hour", "30 1 * * *", ScheduleTypeDaily},
		{"weekly sunday", "0 2 * * 0", ScheduleTypeWeekly},
		{"weekly monday", "0 2 * * 1", ScheduleTypeWeekly},
		{"weekly saturday", "0 2 * * 6", ScheduleTypeWeekly},
		{"monthly", "0 2 1 * *", ScheduleTypeMonthly},
		{"monthly different hour", "30 1 1 * *", ScheduleTypeMonthly},
		{"yearly jan 1", "0 2 1 1 *", ScheduleTypeYearly},
		{"yearly jul 1", "0 2 1 7 *", ScheduleTypeYearly},
		{"custom multi daily", "0 */6 * * *", ScheduleTypeCustom},
		{"custom every 5 min", "*/5 * * * *", ScheduleTypeCustom},
		{"invalid too short", "0 3 * *", ScheduleTypeCustom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := ScheduleConfig{Cron: tt.cron}
			got := s.ScheduleType()
			if got != tt.expected {
				t.Errorf("ScheduleType() = %q, want %q (cron=%q)", got, tt.expected, tt.cron)
			}
		})
	}
}

func TestScheduleConfig_ScheduleDir(t *testing.T) {
	tests := []struct {
		name     string
		cron     string
		time     time.Time
		expected string
	}{
		{
			name:     "daily",
			cron:     "0 3 * * *",
			time:     time.Date(2026, 4, 6, 3, 0, 0, 0, time.UTC),
			expected: "daily",
		},
		{
			name:     "weekly",
			cron:     "0 2 * * 1",
			time:     time.Date(2026, 4, 6, 2, 0, 0, 0, time.UTC),
			expected: "weekly/2026-W15",
		},
		{
			name:     "monthly",
			cron:     "0 2 1 * *",
			time:     time.Date(2026, 4, 1, 2, 0, 0, 0, time.UTC),
			expected: "monthly/2026-04",
		},
		{
			name:     "yearly",
			cron:     "0 2 1 1 *",
			time:     time.Date(2026, 1, 1, 2, 0, 0, 0, time.UTC),
			expected: "yearly/2026",
		},
		{
			name:     "custom returns empty",
			cron:     "*/5 * * * *",
			time:     time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC),
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := ScheduleConfig{Cron: tt.cron}
			got := s.ScheduleDir(tt.time)
			if got != tt.expected {
				t.Errorf("ScheduleDir() = %q, want %q", got, tt.expected)
			}
		})
	}
}
