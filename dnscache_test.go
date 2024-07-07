package dnscache

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var (
	testFreq                 = 1 * time.Second
	testDefaultLookupTimeout = 1 * time.Second
)

func testResolver(t *testing.T) *Resolver {
	t.Helper()
	r, err := New(testFreq, testDefaultLookupTimeout)
	if err != nil {
		t.Fatalf("err: %s", err)
	}
	return r
}

func TestNew(t *testing.T) {
	{
		resolver, err := New(testFreq, testDefaultLookupTimeout)
		if err != nil {
			t.Fatalf("expect not to be failed")
		}
		resolver.Stop()
	}

	{
		resolver, err := New(0, 0)
		if err != nil {
			t.Fatalf("expect not to be failed")
		}
		resolver.Stop()
	}
}

func TestLookup(t *testing.T) {
	cases := []struct {
		name string
	}{
		{
			"api.mercari.jp",
		},
		{
			"ya.ru",
		},
		{
			"google.com",
		},
	}

	resolver := testResolver(t)
	defer resolver.Stop()
	for _, tc := range cases {
		ips, err := resolver.LookupIP(context.Background(), tc.name)
		if err != nil {
			t.Fatalf("err: %s", err)
		}
		if len(ips) == 0 {
			t.Fatalf("got no records")
		}

		for _, ip := range ips {
			if ip.IP.To4() == nil && ip.IP.To16() == nil {
				t.Fatalf("got %v; want an IP address", ip)
			}
		}
	}
}

func TestLookupCache(t *testing.T) {
	originalFunc := lookupIP
	defer func() {
		lookupIP = originalFunc
	}()

	netaddr := net.IPAddr{}
	netaddr.IP = net.IP("35.190.50.136")
	want := []net.IPAddr{
		netaddr,

	}
	lookupIP = func(ctx context.Context, host string) ([]net.IPAddr, error) {
		return want, nil
	}

	ctx := context.Background()
	resolver := testResolver(t)
	defer resolver.Stop()

	got, err := resolver.LookupIP(ctx, "gateway.io")
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	if !reflect.DeepEqual(want, got) {
		t.Fatalf("want %#v, got %#v", want, got)
	}

	got2, ok := resolver.cache["gateway.io"]
	if !ok {
		t.Fatalf("expect cache to be created")
	}

	if !reflect.DeepEqual(want, got2) {
		t.Fatalf("want %#v, got %#v", want, got2)
	}
}

func TestLookupTimeout(t *testing.T) {
	originalFunc := lookupIP
	defer func() {
		lookupIP = originalFunc
	}()

	ctx, cancelF := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelF()
	lookupIP = func(ctx context.Context, host string) ([]net.IPAddr, error) {
		for {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
			time.Sleep(200 * time.Millisecond)
		}
	}

	resolver := testResolver(t)
	defer resolver.Stop()

	_, err := resolver.LookupIP(ctx, "gateway.io")
	if err == nil {
		t.Fatalf("expect to be failed")
	}
}

func TestRefresh(t *testing.T) {
	originalFunc := lookupIP
	defer func() {
		lookupIP = originalFunc
	}()

	netaddr := net.IPAddr{}
	netaddr.IP = net.IP("4.4.4.4")
	want := []net.IPAddr{
		netaddr,

	}

	lookupIP = func(ctx context.Context, host string) ([]net.IPAddr, error) {
		return want, nil
	}

	netaddr1 := net.IPAddr{}
	netaddr1.IP = net.IP("1.1.1.1")

	netaddr2 := net.IPAddr{}
	netaddr2.IP = net.IP("2.2.2.2")

	netaddr3 := net.IPAddr{}
	netaddr3.IP = net.IP("3.3.3.3")

	resolver := testResolver(t)
	defer resolver.Stop()
	resolver.cache = map[string][]net.IPAddr{
		"deeeet.jp": {
			netaddr1,
		},
		"deeeet.us": {
			netaddr2,
		},
		"deeeet.uk": {
			netaddr3,
		},
	}

	// Refresh all IP to same one
	resolver.Refresh()

	// Ensure all cache are refreshed
	for _, got := range resolver.cache {
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("want %#v, got %#v", want, got)
		}
	}
}

func TestRefreshed(t *testing.T) {
	originalFunc := onRefreshed
	defer func() {
		onRefreshed = originalFunc
	}()

	var counter int32
	onRefreshed = func() {
		atomic.AddInt32(&counter, 1)
	}

	resolver, err := New(1*time.Millisecond, testDefaultLookupTimeout)
	defer resolver.Stop()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	time.Sleep(10 * time.Millisecond)

	cnt := atomic.LoadInt32(&counter)
	if cnt < 5 {
		t.Fatalf("Not refreshed enough: %d", cnt)
	}
}

func TestFetch(t *testing.T) {
	mu := new(sync.Mutex)

	originalFunc := lookupIP
	defer func() {
		lookupIP = originalFunc
	}()

	var returnIPs []net.IPAddr
	lookupIP = func(ctx context.Context, host string) ([]net.IPAddr, error) {
		mu.Lock()
		ips := returnIPs
		mu.Unlock()
		return ips, nil
	}

	ctx := context.Background()
	resolver := testResolver(t)
	defer resolver.Stop()

	

	netaddr := net.IPAddr{}
	netaddr.IP = net.IP("10.0.0.1")
	want1 := []net.IPAddr{
		netaddr,

	}
	
	mu.Lock()
	returnIPs = want1
	mu.Unlock()

	got1, err := resolver.Fetch(ctx, "test.com")
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	if !reflect.DeepEqual(want1, got1) {
		t.Fatalf("want %#v, got %#v", want1, got1)
	}


	netaddr2 := net.IPAddr{}
	netaddr2.IP = net.IP("10.0.0.2")
	want2 := []net.IPAddr{
		netaddr2,

	}
	mu.Lock()
	returnIPs = want2
	mu.Unlock()

	got2, err := resolver.Fetch(ctx, "test.com")
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	// Cache should be used
	if !reflect.DeepEqual(want1, got2) {
		t.Fatalf("want %#v, got %#v", want1, got2)
	}

	// Wait until cache is refreshed
	time.Sleep(2 * time.Second)

	got3, err := resolver.Fetch(ctx, "test.com")
	if err != nil {
		t.Fatalf("err: %s", err)
	}
	// Cache should be refreshed
	if !reflect.DeepEqual(want2, got3) {
		t.Fatalf("want %#v, got %#v", want2, got3)
	}
}

func TestErrorLog(t *testing.T) {
	originalFunc1 := lookupIP
	defer func() {
		lookupIP = originalFunc1
	}()

	originalFunc2 := onRefreshed
	defer func() {
		onRefreshed = originalFunc2
	}()

	done := make(chan struct{}, 1)
	onRefreshed = func() {
		done <- struct{}{}
	}

	lookupIP = func(ctx context.Context, host string) ([]net.IPAddr, error) {
		return nil, fmt.Errorf("err")
	}

	buf := new(bytes.Buffer)
	logger := slog.New(slog.NewTextHandler(buf, nil))

	resolver, err := New(0, 0, WithLogger(logger))
	if err != nil {
		t.Fatalf("err: %s", err)
	}
	defer resolver.Stop()

	<-done
	if got, want := buf.Len(), 1; got >= want {
		t.Fatalf("expect logger called more than once")
	}
}
