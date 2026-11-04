package catalog

import "context"

func (s *PreferredSync) TestSetFetcher(f func(context.Context, string, string) (string, error)) {
	s.fetch = f
}
