package replicate

import (
	"bufio"
	"bytes"
	"strings"
)

// refStatus is the outcome of one ref in "git push --porcelain".
type refStatus int

const (
	refOK refStatus = iota
	// refStale means --force-with-lease did not match the remote value.
	refStale
	refFailed
)

type refResult struct {
	status  refStatus
	summary string
}

// parsePorcelain parses the output of "git push --porcelain" into results
// keyed by the remote ref name.
func parsePorcelain(out []byte) map[string]refResult {
	results := map[string]refResult{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		if len(line) < 2 || line[1] != '\t' {
			continue
		}
		flag := line[0]
		fields := strings.SplitN(line[2:], "\t", 2)
		if len(fields) < 1 {
			continue
		}
		_, to, ok := strings.Cut(fields[0], ":")
		if !ok || to == "" {
			continue
		}
		summary := ""
		if len(fields) == 2 {
			summary = fields[1]
		}
		res := refResult{status: refOK, summary: summary}
		if flag == '!' {
			switch {
			case strings.Contains(summary, "stale info"):
				res.status = refStale
			case strings.Contains(summary, "remote ref does not exist"):
				// Deleting a ref that is already gone.
				res.status = refOK
			default:
				res.status = refFailed
			}
		} else if !strings.ContainsRune(" +-*=", rune(flag)) {
			res.status = refFailed
		}
		results[to] = res
	}
	return results
}
