package forward

import (
	"context"
	"fmt"
	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/request"
	"github.com/miekg/dns"
	ot "github.com/opentracing/opentracing-go"
	"github.com/sparrc/go-ping"
	"net"
	"sync"
	"time"
)

var locker sync.RWMutex

func (f *Forward) findBest(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	state := request.Request{W: w, Req: r}
	if !f.match(state) {
		return plugin.NextOrFailure(f.Name(), f.Next, ctx, w, r)
	}

	proxyc := make(chan BestResult, 1)
	listFinished := 0
	ipMap := make(map[string]bool)
	for _, proxy := range f.List() {
		go f.getProxyPing(proxy, proxyc, ipMap, ctx, w, r)
	}
	t := time.NewTimer(30 * time.Second)
	select {
	case v1 := <-proxyc:
		listFinished += 1
		if listFinished >= f.Len() {
			break
		}
		fmt.Print(v1)
	case <-t.C:
		break
	}
	close(proxyc)
	t.Stop()
	//fails := 0
	//var span, child ot.Span
	var upstreamErr error

	if upstreamErr != nil {
		return dns.RcodeServerFailure, upstreamErr
	}

	return dns.RcodeServerFailure, ErrNoHealthy
}

type BestResult struct {
	IP    net.IP
	Ping  int
	Ret   *dns.Msg
	Stats *ping.Statistics
}

func (f *Forward) getProxyPing(proxy *Proxy, outc chan BestResult, ipMap map[string]bool,
	ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (br BestResult) {
	maxFails := 3

	var span, child ot.Span
	span = ot.SpanFromContext(ctx)
	state := request.Request{W: w, Req: r}
	for fails := 0; fails < maxFails; fails++ {
		//if proxy.Down(f.maxfails) {
		//	continue
		//}
		//// All upstream proxies are dead, assume healtcheck is completely broken and randomly
		//// select an upstream to connect to.
		//r := new(random)
		//proxy = r.List(f.proxies)[0]
		//
		//HealthcheckBrokenCount.Add(1)

		if span != nil {
			child = span.Tracer().StartSpan("connect", ot.ChildOf(span.Context()))
			ctx = ot.ContextWithSpan(ctx, child)
		}

		var (
			ret *dns.Msg
			err error
		)
		opts := f.opts
		for {
			ret, err = proxy.Connect(ctx, state, opts)
			if err == nil {
				break
			}
			if err == ErrCachedClosed { // Remote side closed conn, can only happen with TCP.
				continue
			}
			// Retry with TCP if truncated and prefer_udp configured.
			if ret != nil && ret.Truncated && !opts.forceTCP && f.opts.preferUDP {
				opts.forceTCP = true
				continue
			}
			break
		}

		if child != nil {
			child.Finish()
		}
		if ret != nil {
			br.Ret = ret
			break
		}
	}
	if br.Ret != nil {
		for _, ans := range br.Ret.Answer {
			switch ans.Header().Rrtype {
			case dns.TypeA:
				answerA := ans.(*dns.A)
				br.IP = answerA.A
				ipStr := br.IP.String()
				locker.Lock()
				if _, ok := ipMap[ipStr]; ok {
					locker.Unlock()
				} else {
					ipMap[ipStr] = true
					locker.Unlock()
					// ping
					getPing(ipStr)
				}
			}
		}
	}
	defer func() {
		if recover() != nil {

		}
	}()
	outc <- br
	return
}

func getPing(ip string) (*ping.Statistics) {
	log.Debugf("ping %s", ip)
	pinger, err := ping.NewPinger(ip)
	if err != nil {
		panic(err)
	}
	pinger.Debug = true
	pinger.Timeout = 10 * time.Second
	pinger.Count = 3
	pinger.Run()                 // blocks until finished
	stats := pinger.Statistics() // get send/receive/rtt stats
	return stats
}
