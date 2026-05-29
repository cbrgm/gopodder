package gopodder

import "time"

type Metrics interface {
	IncHTTPRequest(method, path string, status int)
	ObserveHTTPDuration(method, path string, duration time.Duration)
	IncSyncOperation(typ string)
}

type noopMetrics struct{}

func (noopMetrics) IncHTTPRequest(string, string, int) {}

func (noopMetrics) ObserveHTTPDuration(string, string, time.Duration) {}

func (noopMetrics) IncSyncOperation(string) {}
