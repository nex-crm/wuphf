package team

import (
	"fmt"
	"testing"
)

// BenchmarkFindChannelLocked measures the O(N) linear scan in
// findChannelLocked. Run before and after adding a channelIndex map
// to quantify the improvement:
//
//	go test -bench=BenchmarkFindChannelLocked -count=5 ./internal/team > before.txt
//	# ... apply fix ...
//	go test -bench=BenchmarkFindChannelLocked -count=5 ./internal/team > after.txt
//	benchstat before.txt after.txt
func BenchmarkFindChannelLocked(b *testing.B) {
	for _, n := range []int{10, 50, 100, 500, 1000} {
		b.Run(fmt.Sprintf("channels=%d", n), func(b *testing.B) {
			br := &Broker{}
			for i := 0; i < n; i++ {
				br.channels = append(br.channels, teamChannel{
					Slug: fmt.Sprintf("channel-%d", i),
				})
			}
			// Worst case: always look up the last element.
			target := fmt.Sprintf("channel-%d", n-1)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				br.findChannelLocked(target)
			}
		})
	}
}

// BenchmarkFindMemberLocked provides an O(1) baseline for comparison.
// findMemberLocked already uses a memberIndex map — the ns/op should
// stay flat across sizes, unlike findChannelLocked.
func BenchmarkFindMemberLocked(b *testing.B) {
	for _, n := range []int{10, 50, 100, 500, 1000} {
		b.Run(fmt.Sprintf("members=%d", n), func(b *testing.B) {
			br := &Broker{}
			for i := 0; i < n; i++ {
				br.members = append(br.members, officeMember{
					Slug: fmt.Sprintf("member-%d", i),
				})
			}
			// memberIndex is rebuilt lazily on first call.
			target := fmt.Sprintf("member-%d", n-1)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				br.findMemberLocked(target)
			}
		})
	}
}
