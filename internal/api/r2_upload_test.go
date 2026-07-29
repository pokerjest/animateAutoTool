package api

import (
	"bytes"
	"io"
	"testing"
)

func TestCountingReaderIsSeekableForR2Retries(t *testing.T) {
	reader := &CountingReader{
		Reader: bytes.NewReader([]byte("backup payload")),
		Total:  int64(len("backup payload")),
	}

	if _, err := io.ReadAll(reader); err != nil {
		t.Fatalf("read upload body: %v", err)
	}
	if reader.Downloaded != reader.Total {
		t.Fatalf("downloaded = %d, want %d", reader.Downloaded, reader.Total)
	}

	position, err := reader.Seek(0, io.SeekStart)
	if err != nil {
		t.Fatalf("rewind upload body: %v", err)
	}
	if position != 0 || reader.Downloaded != 0 {
		t.Fatalf("rewind position = %d, downloaded = %d; want both 0", position, reader.Downloaded)
	}

	payload, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read upload body after rewind: %v", err)
	}
	if string(payload) != "backup payload" {
		t.Fatalf("payload after rewind = %q", payload)
	}
}
