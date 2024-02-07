// Copyright (c) 2024 Alexander Arkhipov <aa@manpager.org>
//
// Permission to use, copy, modify, and distribute this software for any
// purpose with or without fee is hereby granted, provided that the above
// copyright notice and this permission notice appear in all copies.
//
// THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
// WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
// MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
// ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
// WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
// ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
// OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.

package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"
)

var qflag = flag.Bool("q", false, "be quiet")
var pflag = flag.Int("p", 1, "number of parallel downloads")

type filename struct {
	n        int      // how many times the url has been accessed
	name     string   // end-file name
	tmpfiles []string // temporary file names
}

// filemap maps URLs to corresponding filenames
var filemap = make(map[string]filename)

func getUrl(url, f string, ch chan int) {
	defer func() { ch <- 0 }()

	rm := func() {
		os.Remove(f)
	}

	if !*qflag {
		fmt.Println("GET", url)
	}

	fp, err := os.Create(f)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		rm()
		return
	}
	defer fp.Close()
	fmt.Println("created", fp.Name())

	resp, err := http.Get(url)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		rm()
		return
	}

	buf := make([]byte, 4096)
	reader := bufio.NewReader(resp.Body)
	writer := bufio.NewWriter(fp)

	for readErr := error(nil); readErr == nil; {
		n, readErr := io.ReadFull(reader, buf)
		if readErr == io.EOF {
			break
		}
		if readErr != nil && readErr != io.ErrUnexpectedEOF {
			fmt.Fprintln(os.Stderr, readErr)
			rm()
			break
		}

		_, writeErr := writer.Write(buf[:n])
		if writeErr != nil {
			fmt.Fprintln(os.Stderr, writeErr)
			rm()
			break
		}
	}
	writer.Flush()
}

func prepUrl(url, d string) (string, error) {
	if !(strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")) {
		url = "http://" + url
	}

	fmentry := filemap[url]
	defer func() { filemap[url] = fmentry }()

	var fname string

	_, fname, _ = strings.Cut(url, "://")
	_, fname, _ = strings.Cut(fname, "/")
	parts := strings.Split(fname, "/")
	fname = parts[len(parts)-1]
	if fname == "" {
		fname = "index.html"
	}

	tmpfp, err := os.CreateTemp(d, fname+"*")
	if err != nil {
		return "", err
	}
	defer tmpfp.Close()

	fmentry.name = fname
	fmentry.tmpfiles = append(fmentry.tmpfiles, tmpfp.Name())

	return url, nil
}

func main() {
	flag.Parse()

	if *pflag < 1 {
		fmt.Fprintln(os.Stderr, "can't do less than 1 parallel downloads")
		os.Exit(1)
	}

	var urls []string

	tmpdir, err := os.MkdirTemp(".", ".goget*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer func() {
		rename := func(url string) {
			fentry := filemap[url]
			defer func() {
				fentry.tmpfiles = fentry.tmpfiles[1:]
				filemap[url] = fentry
			}()
			os.Rename(fentry.tmpfiles[0], fentry.name)
			// Ignoring ErrNotExist since the temporary file might
			// have been removed on purpose.
			//
			// TODO It would be better to have such purposeful
			// removals marked explicitly.
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				fmt.Fprintln(os.Stderr, err)
			}
		}

		for _, url := range urls {
			rename(url)
		}

		err := os.Remove(tmpdir)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}()

	for _, arg := range flag.Args() {
		url, err := prepUrl(arg, tmpdir)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			continue
		}
		urls = append(urls, url)
	}

	ch := make(chan int, *pflag)
	routines := 0
	for _, url := range urls {
		if routines >= *pflag {
			<-ch
			routines--
		}
		if fmentry, ok := filemap[url]; ok {
			go getUrl(url, fmentry.tmpfiles[fmentry.n], ch)
			fmentry.n++
			filemap[url] = fmentry
			routines++
		}
	}

	for routines > 0 {
		<-ch
		routines--
	}
}
