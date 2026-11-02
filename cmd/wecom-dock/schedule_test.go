package main

import (
	"testing"
	"time"
)

func TestNextDaily(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	morning := time.Date(2026, 11, 2, 9, 0, 0, 0, loc)
	got := nextDaily(morning, 10, 30, loc)
	want := time.Date(2026, 11, 2, 10, 30, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("morning: got %s want %s", got, want)
	}

	afternoon := time.Date(2026, 11, 2, 10, 30, 0, 0, loc)
	got = nextDaily(afternoon, 10, 30, loc)
	want = time.Date(2026, 11, 3, 10, 30, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("exact: got %s want %s", got, want)
	}

	utc := time.Date(2026, 11, 2, 2, 0, 0, 0, time.UTC) // 10:00 in Shanghai
	got = nextDaily(utc, 10, 30, loc)
	want = time.Date(2026, 11, 2, 10, 30, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("utc: got %s want %s", got, want)
	}
}
