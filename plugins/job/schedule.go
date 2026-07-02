// Package job — schedule domain logic behind the TUI ScheduleBuilder: a
// friendly frequency/time/day model that compiles to/from a cron string.
// Framework-free so it can be unit tested without any TUI dependency.
package job

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Frequency is the schedule builder's coarse cron shape.
type Frequency string

const (
	FreqOff     Frequency = "off"
	FreqHourly  Frequency = "hourly"
	FreqDaily   Frequency = "daily"
	FreqWeekly  Frequency = "weekly"
	FreqMonthly Frequency = "monthly"
	FreqCustom  Frequency = "custom"
)

// DayPreset is a day-of-week choice offered for the weekly frequency.
type DayPreset string

const (
	DayWeekdays DayPreset = "weekdays"
	DayWeekend  DayPreset = "weekend"
	DaySun      DayPreset = "sun"
	DayMon      DayPreset = "mon"
	DayTue      DayPreset = "tue"
	DayWed      DayPreset = "wed"
	DayThu      DayPreset = "thu"
	DayFri      DayPreset = "fri"
	DaySat      DayPreset = "sat"
)

// DayPresets is the ordered list for cycling in the builder.
var DayPresets = []DayPreset{DayWeekdays, DayWeekend, DayMon, DayTue, DayWed, DayThu, DayFri, DaySat, DaySun}

// DayPresetLabel is the human label for each day preset.
var DayPresetLabel = map[DayPreset]string{
	DayWeekdays: "Mon–Fri", DayWeekend: "Sat–Sun",
	DaySun: "Sunday", DayMon: "Monday", DayTue: "Tuesday", DayWed: "Wednesday",
	DayThu: "Thursday", DayFri: "Friday", DaySat: "Saturday",
}

var dowCron = map[DayPreset]string{
	DayWeekdays: "1-5", DayWeekend: "0,6",
	DaySun: "0", DayMon: "1", DayTue: "2", DayWed: "3", DayThu: "4", DayFri: "5", DaySat: "6",
}

// ScheduleSpec is the friendly model the ScheduleBuilder edits.
type ScheduleSpec struct {
	Frequency Frequency
	Minute    int // 0-59
	Hour      int // 0-23
	DayPreset DayPreset
	Dom       int // 1-31, monthly only
	Custom    string
}

// DefaultSchedule is the builder's initial state ("off").
var DefaultSchedule = ScheduleSpec{Frequency: FreqOff, Minute: 0, Hour: 8, DayPreset: DayWeekdays, Dom: 1}

func pad2(n int) string { return fmt.Sprintf("%02d", n) }

// BuildCron composes a cron string from a spec. "off" builds "" (no trigger).
func BuildCron(spec ScheduleSpec) string {
	switch spec.Frequency {
	case FreqOff:
		return ""
	case FreqCustom:
		return strings.TrimSpace(spec.Custom)
	case FreqHourly:
		return fmt.Sprintf("%d * * * *", spec.Minute)
	case FreqDaily:
		return fmt.Sprintf("%d %d * * *", spec.Minute, spec.Hour)
	case FreqWeekly:
		return fmt.Sprintf("%d %d * * %s", spec.Minute, spec.Hour, dowCron[spec.DayPreset])
	case FreqMonthly:
		return fmt.Sprintf("%d %d %d * *", spec.Minute, spec.Hour, spec.Dom)
	}
	return ""
}

var plainIntRe = regexp.MustCompile(`^\d+$`)

func intOrNil(s string) (int, bool) {
	if !plainIntRe.MatchString(s) {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

// ParseCron is a best-effort parse of a cron string back into a ScheduleSpec.
// Anything outside the simple model (ranges, Jenkins H, step values, a
// non-"*" month) round-trips as "custom" so the raw string is preserved.
func ParseCron(cron string) ScheduleSpec {
	trimmed := strings.TrimSpace(cron)
	if trimmed == "" {
		return DefaultSchedule
	}
	asCustom := func() ScheduleSpec {
		s := DefaultSchedule
		s.Frequency = FreqCustom
		s.Custom = trimmed
		return s
	}

	parts := strings.Fields(trimmed)
	if len(parts) != 5 {
		return asCustom()
	}
	min, hr, dom, mon, dow := parts[0], parts[1], parts[2], parts[3], parts[4]

	if mon != "*" {
		return asCustom()
	}
	minN, ok := intOrNil(min)
	if !ok {
		return asCustom()
	}

	spec := DefaultSchedule
	spec.Minute = minN

	if dom != "*" {
		domN, domOk := intOrNil(dom)
		hrN, hrOk := intOrNil(hr)
		if !domOk || !hrOk || dow != "*" {
			return asCustom()
		}
		spec.Frequency = FreqMonthly
		spec.Dom = domN
		spec.Hour = hrN
		return spec
	}

	if dow != "*" {
		var preset DayPreset
		found := false
		for k, v := range dowCron {
			if v == dow {
				preset, found = k, true
				break
			}
		}
		hrN, hrOk := intOrNil(hr)
		if !found || !hrOk {
			return asCustom()
		}
		spec.Frequency = FreqWeekly
		spec.DayPreset = preset
		spec.Hour = hrN
		return spec
	}

	if hr != "*" {
		hrN, hrOk := intOrNil(hr)
		if !hrOk {
			return asCustom()
		}
		spec.Frequency = FreqDaily
		spec.Hour = hrN
		return spec
	}

	spec.Frequency = FreqHourly
	return spec
}

// DescribeSchedule is a short human summary shown under the cron preview.
func DescribeSchedule(spec ScheduleSpec) string {
	at := pad2(spec.Hour) + ":" + pad2(spec.Minute)
	switch spec.Frequency {
	case FreqOff:
		return "No schedule — runs only when triggered."
	case FreqCustom:
		if strings.TrimSpace(spec.Custom) != "" {
			return "Custom cron expression."
		}
		return "Custom (empty)."
	case FreqHourly:
		return "Every hour at :" + pad2(spec.Minute) + "."
	case FreqDaily:
		return "Every day at " + at + "."
	case FreqWeekly:
		return "Every " + DayPresetLabel[spec.DayPreset] + " at " + at + "."
	case FreqMonthly:
		return fmt.Sprintf("Day %d of every month at %s.", spec.Dom, at)
	}
	return ""
}
