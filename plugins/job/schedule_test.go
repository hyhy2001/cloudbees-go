package job

import "testing"

func TestBuildCronRoundTrip(t *testing.T) {
	cases := []struct {
		spec ScheduleSpec
		cron string
	}{
		{ScheduleSpec{Frequency: FreqOff}, ""},
		{ScheduleSpec{Frequency: FreqHourly, Minute: 15}, "15 * * * *"},
		{ScheduleSpec{Frequency: FreqDaily, Minute: 0, Hour: 8}, "0 8 * * *"},
		{ScheduleSpec{Frequency: FreqWeekly, Minute: 30, Hour: 9, DayPreset: DayWeekdays}, "30 9 * * 1-5"},
		{ScheduleSpec{Frequency: FreqMonthly, Minute: 0, Hour: 0, Dom: 1}, "0 0 1 * *"},
		{ScheduleSpec{Frequency: FreqCustom, Custom: "*/5 * * * *"}, "*/5 * * * *"},
	}
	for _, c := range cases {
		if got := BuildCron(c.spec); got != c.cron {
			t.Errorf("BuildCron(%+v) = %q, want %q", c.spec, got, c.cron)
		}
		if c.spec.Frequency == FreqOff || c.spec.Frequency == FreqCustom {
			continue // "" parses back to off, custom cron with steps doesn't round-trip
		}
		parsed := ParseCron(c.cron)
		if parsed.Frequency != c.spec.Frequency {
			t.Errorf("ParseCron(%q).Frequency = %v, want %v", c.cron, parsed.Frequency, c.spec.Frequency)
		}
	}
}

func TestParseCronFallsBackToCustom(t *testing.T) {
	for _, cron := range []string{"H H * * *", "*/5 * * * *", "0 0 * 3 *", "not a cron"} {
		spec := ParseCron(cron)
		if spec.Frequency != FreqCustom || spec.Custom != cron {
			t.Errorf("ParseCron(%q) = %+v, want custom %q", cron, spec, cron)
		}
	}
}
