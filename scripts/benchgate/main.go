// Command benchgate compares two `go test -bench` outputs and fails when a
// benchmark got more than -max slower (median ns/op), the M8 CI gate.
//
//	go run ./scripts/benchgate -max 0.30 base.txt head.txt
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
)

func main() {
	limit := flag.Float64("max", 0.30, "largest allowed slowdown (0.30 = 30%)")
	flag.Parse()
	if flag.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "usage: benchgate [-max 0.30] base.txt head.txt")
		os.Exit(2)
	}
	base, err1 := read(flag.Arg(0))
	head, err2 := read(flag.Arg(1))
	if err1 != nil || err2 != nil {
		fmt.Fprintln(os.Stderr, err1, err2)
		os.Exit(2)
	}
	names := make([]string, 0, len(head))
	for n := range head {
		names = append(names, n)
	}
	slices.Sort(names)
	failed := false
	fmt.Printf("%-40s %14s %14s %8s\n", "benchmark", "base ns/op", "head ns/op", "delta")
	for _, n := range names {
		b, ok := base[n]
		if !ok {
			fmt.Printf("%-40s %14s %14.0f %8s\n", n, "new", median(head[n]), "")
			continue
		}
		bm, hm := median(b), median(head[n])
		delta := hm/bm - 1
		mark := ""
		if delta > *limit {
			mark, failed = "  FAIL", true
		}
		fmt.Printf("%-40s %14.0f %14.0f %+7.1f%%%s\n", n, bm, hm, delta*100, mark)
	}
	if failed {
		fmt.Printf("benchmarks regressed by more than %.0f%%\n", *limit*100)
		os.Exit(1)
	}
}

// read collects ns/op per benchmark name (CPU suffix stripped).
func read(path string) (map[string][]float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string][]float64{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 4 || !strings.HasPrefix(fields[0], "Benchmark") {
			continue
		}
		for i := 2; i+1 < len(fields); i++ {
			if fields[i+1] == "ns/op" {
				v, err := strconv.ParseFloat(fields[i], 64)
				if err == nil {
					name := fields[0]
					if j := strings.LastIndex(name, "-"); j > 0 {
						name = name[:j]
					}
					out[name] = append(out[name], v)
				}
			}
		}
	}
	return out, sc.Err()
}

func median(v []float64) float64 {
	s := slices.Clone(v)
	slices.Sort(s)
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}
