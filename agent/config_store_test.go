package main

import "encoding/json"

type fakeConfigStore struct {
	values    map[string]interface{}
	ranges    string
	setCounts map[string]int
}

func newFakeConfigStore() *fakeConfigStore {
	return &fakeConfigStore{
		values:    make(map[string]interface{}),
		setCounts: make(map[string]int),
	}
}

func (f *fakeConfigStore) GetRanges() (string, error) { return f.ranges, nil }

func (f *fakeConfigStore) SetRanges(text string) error {
	f.ranges = text
	return nil
}

func (f *fakeConfigStore) GetRangesList() ([]string, error) { return []string{}, nil }

func (f *fakeConfigStore) SetConfigValue(key string, value interface{}) error {
	f.values[key] = value
	f.setCounts[key]++
	return nil
}

func (f *fakeConfigStore) DeleteConfigValue(key string) error {
	delete(f.values, key)
	return nil
}

func (f *fakeConfigStore) GetConfigValue(key string, dest interface{}) error {
	val, ok := f.values[key]
	if !ok {
		return nil
	}
	data, err := json.Marshal(val)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dest)
}

func (f *fakeConfigStore) Close() error { return nil }

func (f *fakeConfigStore) setCount(key string) int {
	return f.setCounts[key]
}
