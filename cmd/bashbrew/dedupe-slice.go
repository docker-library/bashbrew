package main

func sliceHas[T comparable](s []T, t T) bool {
	for _, i := range s {
		if i == t {
			return true
		}
	}
	return false
}

type dedupeSlice[T comparable] struct {
	s []T
}

func (s *dedupeSlice[T]) add(i T) bool {
	if sliceHas[T](s.s, i) {
		return false
	}
	s.s = append(s.s, i)
	return true
}

func (s dedupeSlice[T]) slice() []T {
	return s.s
}

type dedupeSliceMap[K comparable, V comparable] struct {
	m map[K]*dedupeSlice[V]
}

func (m *dedupeSliceMap[K, V]) add(key K, value V) bool {
	if m.m == nil {
		m.m = map[K]*dedupeSlice[V]{}
	}
	if s, ok := m.m[key]; !ok || s == nil {
		m.m[key] = &dedupeSlice[V]{}
	}
	return m.m[key].add(value)
}

func (m dedupeSliceMap[K, V]) has(key K) bool {
	s, ok := m.m[key]
	return ok && s != nil
}

func (m dedupeSliceMap[K, V]) slice(key K) []V {
	if !m.has(key) {
		return nil
	}
	return m.m[key].slice()
}
