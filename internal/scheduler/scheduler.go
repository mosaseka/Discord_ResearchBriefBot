package scheduler

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type Schedule struct {
	fields   [5]map[int]bool
	wildcard [5]bool
}

var limits = [5][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 7}}

func Parse(expression string) (Schedule, error) {
	parts := strings.Fields(expression)
	if len(parts) != 5 {
		return Schedule{}, errors.New("cron must have five fields")
	}
	var s Schedule
	for idx, part := range parts {
		values, wild, err := parseField(part, limits[idx][0], limits[idx][1])
		if err != nil {
			return Schedule{}, fmt.Errorf("cron field %d: %w", idx+1, err)
		}
		s.fields[idx], s.wildcard[idx] = values, wild
		if idx == 4 && values[7] {
			values[0] = true
			delete(values, 7)
		}
	}
	return s, nil
}
func parseField(raw string, minValue, maxValue int) (map[int]bool, bool, error) {
	out := map[int]bool{}
	wild := raw == "*"
	for _, part := range strings.Split(raw, ",") {
		step := 1
		base := part
		if left, right, ok := strings.Cut(part, "/"); ok {
			base = left
			parsed, err := strconv.Atoi(right)
			if err != nil || parsed < 1 {
				return nil, false, errors.New("invalid step")
			}
			step = parsed
		}
		start, end := minValue, maxValue
		if base != "*" {
			if left, right, ok := strings.Cut(base, "-"); ok {
				var err error
				start, err = strconv.Atoi(left)
				if err != nil {
					return nil, false, err
				}
				end, err = strconv.Atoi(right)
				if err != nil {
					return nil, false, err
				}
			} else {
				value, err := strconv.Atoi(base)
				if err != nil {
					return nil, false, err
				}
				start, end = value, value
			}
		}
		if start < minValue || end > maxValue || start > end {
			return nil, false, errors.New("value outside range")
		}
		for value := start; value <= end; value += step {
			out[value] = true
		}
	}
	return out, wild, nil
}
func (s Schedule) Matches(t time.Time) bool {
	weekday := int(t.Weekday())
	basic := s.fields[0][t.Minute()] && s.fields[1][t.Hour()] && s.fields[3][int(t.Month())]
	if !basic {
		return false
	}
	day := s.fields[2][t.Day()]
	week := s.fields[4][weekday]
	if !s.wildcard[2] && !s.wildcard[4] {
		return day || week
	}
	return day && week
}
func (s Schedule) Next(after time.Time) (time.Time, error) {
	candidate := after.Truncate(time.Minute).Add(time.Minute)
	limit := candidate.AddDate(2, 0, 0)
	for candidate.Before(limit) {
		if s.Matches(candidate) {
			return candidate, nil
		}
		candidate = candidate.Add(time.Minute)
	}
	return time.Time{}, errors.New("no cron occurrence within two years")
}

type Runner struct {
	schedule Schedule
	location *time.Location
	running  atomic.Bool
}

func New(expression string, location *time.Location) (*Runner, error) {
	schedule, err := Parse(expression)
	if err != nil {
		return nil, err
	}
	if location == nil {
		location = time.UTC
	}
	return &Runner{schedule: schedule, location: location}, nil
}
func (r *Runner) Run(ctx context.Context, job func(context.Context) error) error {
	for {
		next, err := r.schedule.Next(time.Now().In(r.location))
		if err != nil {
			return err
		}
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if !r.running.CompareAndSwap(false, true) {
			continue
		}
		func() { defer r.running.Store(false); _ = job(ctx) }()
	}
}
