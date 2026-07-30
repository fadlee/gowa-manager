package instancelogs

import (
	"testing"
	"time"
)

func TestStoreTailIsBoundedPerInstanceAndNewestLast(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	store := NewStore(3)

	store.Append(1, StreamStdout, "one", now)
	store.Append(1, StreamStderr, "two", now.Add(time.Second))
	store.Append(2, StreamStdout, "other-instance", now.Add(2*time.Second))
	store.Append(1, StreamStdout, "three", now.Add(3*time.Second))
	store.Append(1, StreamStderr, "four", now.Add(4*time.Second))

	got := store.Tail(1, 10)
	if len(got) != 3 {
		t.Fatalf("Tail len = %d, want 3", len(got))
	}
	wantLines := []string{"two", "three", "four"}
	wantStreams := []Stream{StreamStderr, StreamStdout, StreamStderr}
	for i := range got {
		if got[i].Line != wantLines[i] || got[i].Stream != wantStreams[i] || got[i].InstanceID != 1 {
			t.Fatalf("Tail[%d] = %+v, want line %q stream %q instance 1", i, got[i], wantLines[i], wantStreams[i])
		}
	}
}

func TestLineWriterSplitsCompleteAndPartialLines(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	clock := func() time.Time { return now }
	store := NewStore(10)
	writer := NewLineWriter(store, 7, StreamStderr, clock)

	if n, err := writer.Write([]byte("first\nsecond")); err != nil || n != len("first\nsecond") {
		t.Fatalf("Write returned n=%d err=%v", n, err)
	}
	if n, err := writer.Write([]byte(" continued\nthird\r\n")); err != nil || n != len(" continued\nthird\r\n") {
		t.Fatalf("Write returned n=%d err=%v", n, err)
	}
	writer.Flush()

	got := store.Tail(7, 10)
	if len(got) != 3 {
		t.Fatalf("Tail len = %d, want 3: %+v", len(got), got)
	}
	want := []string{"first", "second continued", "third"}
	for i := range got {
		if got[i].Line != want[i] || got[i].Stream != StreamStderr || !got[i].Timestamp.Equal(now) {
			t.Fatalf("Tail[%d] = %+v, want line %q stderr at %s", i, got[i], want[i], now)
		}
	}
}
