// Package chaos implements a plugin that answer to 'CH version.bind TXT' type queries.
package best

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/sparrc/go-ping"
	"math"
	"net"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/request"

	clog "github.com/coredns/coredns/plugin/pkg/log"
	"github.com/miekg/dns"
)

// Best allows CoreDNS to reply to CH TXT queries and return author or
// version information.
type Best struct {
	Next    plugin.Handler
	Version string
	Authors []string
}

type IpCache struct {
	IP       string
	Host     string
	Status   *ping.Statistics
	Fail     uint8
	LastFail time.Time
}

var log = clog.NewWithPlugin("best")

const pingTimeout = 10 * time.Second
const failedInter = 3 * time.Minute

var locker sync.RWMutex
var cacheIpMaps sync.Map
var bestHostIpMaps sync.Map

// ServeDNS implements the plugin.Handler interface.
func (c Best) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	state := request.Request{W: w, Req: r}
	if state.QType() != dns.TypeA {
		return plugin.NextOrFailure(c.Name(), c.Next, ctx, w, r)
	}

	m := new(dns.Msg)
	m.SetReply(r)
	qname := state.QName()
	var bestIp net.IP
	if ip, ok := bestHostIpMaps.Load(qname); ok {
		if ip == nil { // 等待任务
			time.Sleep(pingTimeout)
			ip, _ = bestHostIpMaps.Load(qname)
			if ip == nil {
				return plugin.NextOrFailure(c.Name(), c.Next, ctx, w, r)
			}
		}
		bestIp = ip.(net.IP)
		log.Debugf("hit best %s[%s]", qname, bestIp.String())
	} else {
		// 标记任务进行中
		bestHostIpMaps.Store(ip, nil)
		ip1, err := getIps(qname)
		if err != nil {
			return plugin.NextOrFailure(c.Name(), c.Next, ctx, w, r)
		}
		bestIp = ip1
		bestHostIpMaps.Store(qname, ip1)
	}

	hdr := dns.RR_Header{Name: state.QName(), Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 0}
	m.Answer = []dns.RR{&dns.A{Hdr: hdr, A: bestIp}}

	w.WriteMsg(m)
	return 0, nil
}

// Name implements the Handler interface.
func (c Best) Name() string { return "best" }

func getIps(host string) (bestIP net.IP, err error) {
	defer func() {
		if err1 := recover(); err1 != nil {
			log.Debug(err1)
			err = errors.New("can't get")
		}
	}()
	pingUrl := "https://tools.ipip.net/ping.php?v=4&a=send&host=" + host + "&area%5B%5D=china&area%5B%5D=hmt&area%5B%5D=asia&area%5B%5D=europe&area%5B%5D=africa&area%5B%5D=na&area%5B%5D=sa&area%5B%5D=da"
	cookie := "LOVEAPP_SESSID=52a68b6f73c7ef789c2dffccc6254a0ffc3fcb14; __jsluid=077d1353716aac038055cea37447aac5; _ga=GA1.2.87212559.1559490717; _gid=GA1.2.1712109698.1559490717; Hm_lvt_123ba42b8d6d2f680c91cb43c1e2be64=1559490717,1559490834; Hm_lpvt_123ba42b8d6d2f680c91cb43c1e2be64=1559490834"
	req, _ := http.NewRequest("GET", pingUrl, nil)
	req.Header.Add("Cookie", cookie)
	req.Header.Add("Referer", "https://tools.ipip.net/ping.php")
	req.Header.Add("Host", "tools.ipip.net")
	client := &http.Client{

	}
	resp, _ := client.Do(req)
	breader := bufio.NewReader(resp.Body)
	ipCh := make(chan *ping.Statistics, 1)
	ipReg, _ := regexp.Compile(`(\d+\.\d+\.\d+\.\d+)`)
	ipMap := make(map[string]bool)
	//ipPingedNum := 0
	waitFullBody := false
	go func() {
		for {
			script, isPrefix, err1 := breader.ReadLine()
			if len(script) > 0 {
				ips := ipReg.FindAllString(string(script), -1)
				for _, ip := range ips {
					if _, ok := ipMap[ip]; !ok {
						ipMap[ip] = true
						ip2 := ip
						go func() {
							defer func() {
								if recover() != nil {

								}
							}()
							ipCh <- getPing(host, ip2)
						}()
					}
				}
			}
			if isPrefix {

			}
			if err1 != nil {
				break
			}
		}
	}()
	var statList []*ping.Statistics
	t := time.NewTimer(30 * time.Second)
	for {
		select {
		case ip, ok := <-ipCh:
			if !ok {
				break
			}
			statList = append(statList, ip)
			// 全部解析
			if len(statList) == len(ipMap) {
				break
			}
			//case <-t.C:
			//	break
		}
		if len(statList) >= len(ipMap) || (!waitFullBody && len(statList) >= 50) {
			break
		}
	}
	close(ipCh)
	t.Stop()
	resp.Body.Close()
	sort.SliceStable(statList, func(i, j int) bool {
		return statList[i].AvgRtt < statList[j].AvgRtt
	})
	//fmt.Print(statList)
	bestIP = statList[0].IPAddr.IP
	log.Debugf("get best %s[%s]", host, bestIP.String())
	return bestIP, nil
}

func test() {
	ipstrs := `216.58.196.238,
216.58.217.206,
172.217.3.206,
216.58.193.142,
172.217.16.142,
74.125.192.100,
108.177.126.138,
172.217.169.14,
172.217.7.142,
74.125.202.101,
172.217.29.14,
172.217.5.78,
172.217.31.142,
172.217.169.14,
216.58.210.238,
172.217.21.142,
172.217.160.110,
64.233.166.138,
216.58.200.78,
216.58.223.142,
74.125.68.101,
172.217.160.78,
216.58.213.14,
172.217.25.110,
74.125.197.113,
172.217.27.46,
64.233.165.100,
216.58.200.46,
216.58.200.78,
172.217.19.238,
172.217.26.14,
172.217.168.238,
172.217.16.238,
172.217.15.110,
74.125.68.138,
216.58.200.78,
216.58.200.110,
216.58.223.238,
216.58.195.238,
216.58.221.206,
172.217.161.174,
216.58.199.78,
64.233.165.100,
172.217.21.46,
173.194.220.139,
172.217.9.14,
172.217.15.78,
64.233.185.139,
172.217.12.110,
209.85.203.102,
173.194.222.138,
216.58.197.142,
172.217.16.174,
74.125.21.102,
216.58.200.238,
172.217.0.46,
216.239.38.120,
172.217.27.46,
216.58.193.142,
172.217.25.14,
172.217.13.110,
172.217.164.110,
172.217.16.238,
216.58.207.46,
216.58.213.14,
172.217.160.110,
172.217.14.78,
216.58.197.110,
172.217.168.206,
172.217.161.142,
172.217.19.206,
173.194.221.138,
216.58.207.206,
172.217.168.238,
216.58.199.78,
172.217.23.142,
216.58.197.206,
216.58.213.174,
172.217.168.238,
172.217.17.174,
74.125.200.113,
74.125.68.139,
216.58.197.174,
172.217.5.206,
172.217.4.78,
172.217.22.238,
172.217.27.142,
172.217.23.174,
172.217.194.100,
172.217.160.110,
172.217.160.78,
216.58.201.142,
172.217.25.174,
172.217.6.174,
172.217.25.78,
172.217.161.142,
216.58.205.238,
172.217.16.174,
172.217.160.206,
216.58.201.174,
216.58.197.110,
172.217.31.142,
172.217.160.110,
216.58.197.238,
172.217.6.174,
172.217.5.78,
172.217.5.14,
172.217.5.206,
216.58.210.206,
216.58.203.206,
172.217.25.46,
172.217.25.110,
74.125.130.100,
216.58.198.110,
172.217.5.238,
172.217.27.142,
216.58.210.14,
64.233.162.101,
172.217.160.46,
172.217.26.14,
172.217.169.142,
216.58.193.206,
216.58.200.46,
172.217.13.142,
216.58.200.14,
172.217.22.14,
216.58.208.110,
172.217.31.238,
216.58.194.174,
172.217.14.206,
172.217.27.142,
172.217.0.46,
172.217.4.46,
172.217.27.78,
74.125.124.102,
172.217.165.14,
216.58.215.78,
172.217.22.174,
172.217.24.14,
172.217.160.110,
172.217.194.101,
172.217.31.238,
216.58.208.110,
216.58.204.78,
172.217.27.142,
172.217.13.174,
173.194.221.100,
172.217.7.206,
172.217.18.14,
216.58.192.14,
172.217.12.174,
172.217.22.78,
108.177.122.101,
172.217.169.46,
172.217.5.238,
216.58.211.14,
172.217.11.78,
64.233.164.139,
172.217.168.174,
209.85.233.138,
172.217.1.206,
216.58.205.206,
172.217.194.139,
172.217.163.238,
172.217.166.78,
172.217.22.174,
172.217.21.142,
172.217.0.238,
216.58.194.174,
172.217.160.110,
172.217.12.14,
172.217.25.142,
173.194.221.139,
64.233.185.101,
64.233.177.102,
216.58.199.174,
172.217.24.14,
172.217.164.174,
216.58.199.78,
172.217.168.206,
216.58.198.14,
172.217.28.174,
172.217.2.14,
172.217.161.46,
172.217.170.78,
172.217.19.206,
172.217.24.174,
172.217.10.142,
172.217.12.174,
216.58.199.14,
172.217.6.206,
172.217.19.14,
216.58.192.46,
216.58.194.174,
172.217.14.238,
172.217.7.142,
216.58.203.110,
172.217.27.142,
172.217.1.14,
172.217.16.14,
172.217.18.206,
216.58.207.206,
172.217.21.174,
172.217.22.174,
172.217.24.206,
216.58.203.174,
216.58.199.174,
172.217.16.174,
172.217.17.142,
216.58.200.14,
172.217.24.206,
216.58.196.14,
172.217.31.238,
172.217.18.174,
216.58.200.78,
172.217.19.110,
172.217.26.46,
172.217.20.206,
172.217.14.78,
172.217.8.174,
216.58.200.14,
172.217.18.78,
172.217.0.142,
216.58.193.206,
172.217.0.238,
216.58.200.78,
172.217.22.142,
172.217.4.174,
172.217.14.78,
172.217.169.14,
172.217.170.46,
172.217.16.14,
172.217.27.78,
216.58.200.78,
172.217.164.110,
172.217.167.78,
172.217.170.46,
172.217.11.238,
216.58.200.46,
64.233.165.139,
172.217.2.238,
172.217.3.206,
172.217.169.14,
216.58.213.110,
216.58.199.110,
172.217.16.174,
172.217.29.142,
172.217.9.14,
172.217.4.46,
216.58.206.14,
172.217.5.78,
216.58.197.142,
216.58.198.78,
172.217.12.142,
172.217.160.110,
172.217.3.110,
74.125.130.113,
172.217.3.174,
216.58.198.238,
172.217.24.206,
216.58.197.110,
173.194.73.113,
172.217.25.174,
216.58.223.142,
172.217.25.174,
173.194.73.102,
172.217.21.142,
108.177.97.100,
172.217.4.174,
74.125.141.138,
172.217.11.174,
216.58.197.46,
216.58.208.174,
172.217.24.206,
172.217.17.78,
216.58.213.142,
172.217.172.206,
173.194.73.101,
172.217.28.14,
216.58.199.46,
172.217.161.174,
172.217.162.110,
216.58.221.206,
216.58.208.46,
74.125.130.101,
172.217.5.110,
172.217.160.78,
216.58.200.78,
172.217.161.174,
172.217.31.238,
172.217.161.174,
216.58.221.238,
216.58.200.238,
74.125.90.110,
216.58.220.206,
172.217.161.174,
216.58.197.110,
172.217.25.14,
172.217.24.46,
172.217.161.174,
172.217.160.78,
172.217.24.14,
172.217.25.14,
172.217.31.238,
172.217.24.46,
172.217.160.78,
216.58.200.46,
216.58.199.14,
216.58.220.206,
172.217.31.238,
172.217.25.14,
172.217.163.238,
216.58.199.14,
216.58.200.14,
172.217.31.238,
172.217.160.78,
216.58.200.14,
172.217.160.78,
216.58.200.238,
172.217.160.110,
216.58.199.14,
172.217.160.78,
172.217.25.14,
172.217.24.14,
216.58.199.110,
216.58.200.78,
172.217.24.14,
216.58.199.14,
216.58.197.110,
172.217.27.142,
172.217.161.142,
172.217.163.238,
172.217.161.174,
216.58.200.78,
216.58.200.46,
216.58.200.238,
172.217.163.238,
172.217.161.174,
216.58.200.78,
216.58.200.46,
172.217.25.14,
216.58.199.110,
172.217.161.174,
216.58.199.14,
216.58.200.46,
172.217.161.142,
172.217.160.110,
216.58.199.14,
172.217.25.14,
216.58.220.206,
172.217.163.238,
216.58.200.46,
172.217.161.142,
216.58.200.14,
216.58.220.206,
216.58.199.14,
216.58.221.238,
172.217.163.238,
172.217.161.142,
66.249.89.104,
172.217.161.174,
216.58.200.46,
172.217.25.14,
172.217.24.46,
216.58.197.110,
172.217.24.14,
172.217.25.14,
172.217.160.78,
216.58.199.14,
172.217.24.14,
172.217.25.14,
172.217.160.110,
216.58.200.238,
172.217.24.206,
216.58.199.14,
172.217.27.142,
172.217.163.238,
172.217.160.78,
172.217.161.142,
172.217.31.238,
216.58.200.14,
172.217.24.14,
172.217.161.174,
216.58.199.110,
172.217.25.14,
216.58.220.206,
172.217.31.238,
172.217.25.14,
216.58.200.46,
172.217.25.14,
172.217.160.110,
216.58.197.110,
172.217.160.78,
172.217.161.174,
216.58.200.14`
	ipMap := make(map[string]bool)
	cnum := 0
	cc := make(chan int, 1)
	statList := []*ping.Statistics{}
	for _, ip := range strings.Split(ipstrs, ",") {
		ip2 := strings.TrimSpace(ip)
		if _, ok := ipMap[ip]; !ok {
			ipMap[ip] = true
			cnum += 1
			go func() {
				stats := getPing("", ip2)
				locker.Lock()
				statList = append(statList, stats)
				locker.Unlock()
				cc <- 1
			}()
		}
	}

	select {
	case <-cc:
		cnum -= 1
		if cnum == 0 {
			break
		}

	}

	sort.SliceStable(statList, func(i, j int) bool {
		return statList[i].AvgRtt < statList[j].AvgRtt
	})
	fmt.Print(statList)
}

func getPing(host, ip string) (*ping.Statistics) {
	// chech cache
	var ipCache IpCache
	ipCache.IP = ip
	if v, ok := cacheIpMaps.LoadOrStore(ip, ipCache); ok {
		ipCache = v.(IpCache)
		// TODO 更好的命中
		if ipCache.Status == nil {
			// 等待其他协程完成
			log.Debugf("waiting %s[%s]\n", host, ip)
			<-time.After(pingTimeout + time.Second)
			if v2, ok2 := cacheIpMaps.Load(ip); ok2 {
				if v2.(IpCache).Status != nil {
					log.Debugf("hit1 %s[%s]\n", host, ip)
					return ipCache.Status
				}
			}
			// 结果无效，重新生成
			//cacheIpMaps.Delete(ip)
			//return getPing(host, ip)
		} else if ipCache.Status.MinRtt > 0 {
			log.Debugf("hit2 %s[%s]\n", host, ip)
			return ipCache.Status
		} else if ipCache.LastFail.Add(failedInter).Before(time.Now()) {
			// 需要刷新
		} else {
			return ipCache.Status
		}
	}

	log.Debugf("ping %s[%s]\n", host, ip)
	pinger, err := ping.NewPinger(ip)
	if err != nil {
		panic(err)
	}
	pinger.Debug = true
	pinger.Timeout = pingTimeout
	pinger.Count = 10
	pinger.Run()                 // blocks until finished
	stats := pinger.Statistics() // get send/receive/rtt stats
	if stats.AvgRtt == 0 {
		stats.AvgRtt = math.MaxInt64
		ipCache.Fail += 1
		ipCache.LastFail = time.Now()
	}
	ipCache.IP = ip
	ipCache.Status = stats
	cacheIpMaps.Store(ip, ipCache)
	return stats
}
