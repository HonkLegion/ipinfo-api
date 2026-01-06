package dataset

import (
	"bytes"
	"encoding/binary"
	"encoding/csv"
	"net"
	"sort"
)

func ParseCSV(data []byte) ([]IPv4Range, []IPv6Range, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1

	rows, err := r.ReadAll()
	if err != nil {
		return nil, nil, err
	}

	var v4 []IPv4Range
	var v6 []IPv6Range

	for _, row := range rows[1:] {
		_, netw, err := net.ParseCIDR(row[0])
		if err != nil {
			continue
		}

		meta := Meta{
			Network:       row[0],
			Country:       row[1],
			CountryCode:   row[2],
			Continent:     row[3],
			ContinentCode: row[4],
			ASN:           row[5],
			ASName:        row[6],
			ASDomain:      row[7],
		}

		if netw.IP.To4() != nil {
			start := binary.BigEndian.Uint32(netw.IP.To4())
			mask := binary.BigEndian.Uint32(netw.Mask)
			end := start | ^mask

			v4 = append(v4, IPv4Range{Start: start, End: end, Meta: meta})
		} else {
			ip := netw.IP.To16()
			startHi := binary.BigEndian.Uint64(ip[:8])
			startLo := binary.BigEndian.Uint64(ip[8:])

			ones, _ := netw.Mask.Size()
			hostBits := 128 - ones

			endHi, endLo := startHi, startLo
			if hostBits >= 64 {
				endLo |= ^uint64(0)
				endHi |= ^uint64(0) << (128 - hostBits)
			} else {
				endLo |= ^uint64(0) >> (64 - hostBits)
			}

			v6 = append(v6, IPv6Range{
				StartHi: startHi, StartLo: startLo,
				EndHi: endHi, EndLo: endLo,
				Meta: meta,
			})
		}
	}

	sort.Slice(v4, func(i, j int) bool { return v4[i].Start < v4[j].Start })
	sort.Slice(v6, func(i, j int) bool {
		if v6[i].StartHi == v6[j].StartHi {
			return v6[i].StartLo < v6[j].StartLo
		}
		return v6[i].StartHi < v6[j].StartHi
	})

	return v4, v6, nil
}
