package daemon

import (
	"context"
	"runtime"
	"testing"
)

func BenchmarkNew(b *testing.B) {
	endpoint := testEndpoint(b)
	b.ReportAllocs()

	for b.Loop() {
		daemon, err := New(Options{Endpoint: endpoint})
		if err != nil {
			b.Fatal(err)
		}

		runtime.KeepAlive(daemon)
		b.StopTimer()
		if err := daemon.Stop(context.Background()); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
}
