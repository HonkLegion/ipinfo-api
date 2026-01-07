package dataset

import (
	"encoding/binary"
	"net"
	"sort"
	"sync/atomic"
)

type Meta struct {
	Network        string `json:"network"`
	Country        string `json:"country"`
	CountryCode    string `json:"country_code"`
	Continent      string `json:"continent"`
	ContinentCode  string `json:"continent_code"`
	ASN            string `json:"asn,omitempty"`
	ASName         string `json:"as_name,omitempty"`
	ASDomain       string `json:"as_domain,omitempty"`
}

type IPv4Range struct {
	Start uint32
	End   uint32
	Meta  Meta
}

type IPv6Range struct {
	StartHi uint64
	StartLo uint64
	EndHi   uint64
	EndLo   uint64
	Meta    Meta
}

type Store struct {
	v4    atomic.Value // []IPv4Range
	v6    atomic.Value // []IPv6Range
	ready atomic.Bool
}

func NewStore() *Store {
	s := &Store{}
	s.v4.Store([]IPv4Range{})
	s.v6.Store([]IPv6Range{})
	s.ready.Store(false)
	return s
}

func (s *Store) Load(v4 []IPv4Range, v6 []IPv6Range) {
	s.v4.Store(v4)
	s.v6.Store(v6)
	s.ready.Store(true)
}

func (s *Store) Ready() bool {
	return s.ready.Load()
}

func (s *Store) Lookup(ip net.IP) *Meta {
	if ip4 := ip.To4(); ip4 != nil {
		val := binary.BigEndian.Uint32(ip4)
		ranges := s.v4.Load().([]IPv4Range)

		i := sort.Search(len(ranges), func(i int) bool {
			return ranges[i].End >= val
		})
		if i < len(ranges) && ranges[i].Start <= val {
			return &ranges[i].Meta
		}
		return nil
	}

	ip = ip.To16()
	hi := binary.BigEndian.Uint64(ip[:8])
	lo := binary.BigEndian.Uint64(ip[8:])
	ranges := s.v6.Load().([]IPv6Range)

	i := sort.Search(len(ranges), func(i int) bool {
		if ranges[i].EndHi == hi {
			return ranges[i].EndLo >= lo
		}
		return ranges[i].EndHi >= hi
	})
	if i < len(ranges) {
		r := ranges[i]
		if hi > r.StartHi || (hi == r.StartHi && lo >= r.StartLo) {
			return &r.Meta
		}
	}
	return nil
}
