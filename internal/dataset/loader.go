package dataset

import "os"

func LoadFromFile(path string) ([]IPv4Range, []IPv6Range, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	return ParseCSV(data)
}
