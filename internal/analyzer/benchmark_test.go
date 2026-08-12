package analyzer

import (
	"os"
	"testing"
)

func BenchmarkAnalyzeSoundOn(b *testing.B) {
	path := os.Getenv("JSHOUND_BENCH_INPUT")
	if path == "" {
		b.Skip("set JSHOUND_BENCH_INPUT to a JavaScript bundle")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		AnalyzeWithOptions(data, Options{})
	}
}
