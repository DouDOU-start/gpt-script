package proxy

import "testing"

func TestPoolRotatesSharedAccountScope(t *testing.T) {
	pool := NewPool(PoolConfig{Enabled: true, Proxies: []string{"p1", "p2"}, RotateOnFailure: true})
	if got := pool.ProxyFor(StageRegister, 0); got != "p1" {
		t.Fatalf("first proxy = %q", got)
	}
	if got := pool.ProxyFor(StageOAuth, 0); got != "p1" {
		t.Fatalf("oauth should share account proxy, got %q", got)
	}
	alloc := pool.MarkFailure(StageRegister, 0, "")
	if alloc == nil || alloc.Proxy != "p2" || alloc.PreviousProxy != "p1" {
		t.Fatalf("unexpected allocation: %#v", alloc)
	}
	if got := pool.ProxyFor(StageTeam, 0); got != "p2" {
		t.Fatalf("team should use rotated proxy, got %q", got)
	}
}

func TestProxyCandidates(t *testing.T) {
	got := ProxyCandidates("host:8080:user:pass")
	if len(got) != 3 || got[0] != "http://user:pass@host:8080" {
		t.Fatalf("unexpected candidates: %#v", got)
	}
}
