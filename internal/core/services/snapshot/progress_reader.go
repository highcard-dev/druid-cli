package services

import (
	"io"
)

type ReaderTracker struct {
	BasicTracker
	reader io.ReadCloser
	read   int64
}

func NewReaderTracker() *ReaderTracker {
	return &ReaderTracker{
		BasicTracker: BasicTracker{},
	}
}

func (bt *ReaderTracker) Read(p []byte) (n int, err error) {
	n, err = bt.reader.Read(p)
	if err != nil {
		return n, err
	}
	bt.read += int64(n)
	bt.LogTrackProgress(bt.read)
	return n, err
}

func (bt *ReaderTracker) Close() error {

	bt.LogTrackProgress(bt.total)
	return bt.reader.Close()
}

func (p *ReaderTracker) TrackProgress(src string, currentSize, totalSize int64, stream io.ReadCloser) (body io.ReadCloser) {
	p.reader = stream
	p.total = totalSize
	return p
}
