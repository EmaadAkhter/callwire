package callwire

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/emaad/callwire/grpcbenchpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type grpcBenchService struct {
	grpcbenchpb.UnimplementedBenchServiceServer
}

func (s *grpcBenchService) Noop(context.Context, *grpcbenchpb.NoopRequest) (*grpcbenchpb.NoopReply, error) {
	return &grpcbenchpb.NoopReply{}, nil
}

func (s *grpcBenchService) Add(_ context.Context, req *grpcbenchpb.AddRequest) (*grpcbenchpb.AddReply, error) {
	return &grpcbenchpb.AddReply{Sum: req.A + req.B}, nil
}

var (
	grpcBenchOnce sync.Once
	grpcBenchAddr = "localhost:9300"
)

func startGRPCBenchServerOnce(tb testing.TB) {
	tb.Helper()
	grpcBenchOnce.Do(func() {
		lis, err := net.Listen("tcp", grpcBenchAddr)
		if err != nil {
			tb.Fatalf("grpc bench listen: %v", err)
		}
		s := grpc.NewServer()
		grpcbenchpb.RegisterBenchServiceServer(s, &grpcBenchService{})
		go func() {
			_ = s.Serve(lis)
		}()
	})
	waitForBenchPort(tb, grpcBenchAddr, 5*time.Second)
}

func grpcBenchClient(tb testing.TB) grpcbenchpb.BenchServiceClient {
	tb.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, grpcBenchAddr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		tb.Fatalf("grpc dial: %v", err)
	}
	tb.Cleanup(func() { _ = conn.Close() })
	return grpcbenchpb.NewBenchServiceClient(conn)
}

func BenchmarkGRPCLatency(b *testing.B) {
	startGRPCBenchServerOnce(b)
	client := grpcBenchClient(b)
	ctx := context.Background()

	benches := []struct {
		name string
		fn   func()
	}{
		{"noop", func() {
			if _, err := client.Noop(ctx, &grpcbenchpb.NoopRequest{}); err != nil {
				b.Fatal(err)
			}
		}},
		{"add", func() {
			if _, err := client.Add(ctx, &grpcbenchpb.AddRequest{A: 10, B: 20}); err != nil {
				b.Fatal(err)
			}
		}},
	}

	for _, bb := range benches {
		b.Run(bb.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				bb.fn()
			}
		})
	}
}

func BenchmarkGRPCThroughputConcurrency(b *testing.B) {
	startGRPCBenchServerOnce(b)
	client := grpcBenchClient(b)
	ctx := context.Background()

	levels := []int{1, 5, 10, 50, 100}
	for _, n := range levels {
		b.Run(fmt.Sprintf("workers-%d", n), func(b *testing.B) {
			var wg sync.WaitGroup
			work := make(chan struct{})

			b.ResetTimer()
			for w := 0; w < n; w++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for range work {
						if _, err := client.Noop(ctx, &grpcbenchpb.NoopRequest{}); err != nil {
							b.Error(err)
						}
					}
				}()
			}
			for i := 0; i < b.N; i++ {
				work <- struct{}{}
			}
			close(work)
			wg.Wait()
		})
	}
}
