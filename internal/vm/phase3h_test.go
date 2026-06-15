package vm_test

import (
	"strings"
	"testing"
)

func TestRetry(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{
			"succeeds_after_retries",
			"a = 0\nr = begin\n  a += 1\n  raise \"f\" if a < 3\n  \"ok\"\nrescue\n  retry if a < 3\n  \"gave up\"\nend\np r\np a",
			"\"ok\"\n3\n",
		},
		{
			"gives_up",
			"c = 0\nr = begin\n  c += 1\n  raise \"x\"\nrescue\n  retry if c < 2\n  \"stopped\"\nend\np r\np c",
			"\"stopped\"\n2\n",
		},
		{
			"ensure_runs_once",
			"log = []\nn = 0\nbegin\n  n += 1\n  log << n\n  raise \"x\" if n < 2\nrescue\n  retry if n < 2\nensure\n  log << 0\nend\np log",
			"[1, 2, 0]\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestRetryOutsideRescue(t *testing.T) {
	if err := runErr(t, `retry`); err == nil || !strings.Contains(err.Error(), "Invalid retry") {
		t.Fatalf("got %v, want an Invalid retry error", err)
	}
}
