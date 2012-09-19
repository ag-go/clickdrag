package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// Flags
var (
	dir    = flag.String("dir", "out", "output directory")
	local  = flag.Bool("local", false, "Only examine local files")
	vLevel = flag.Int("v", 0, "Verbose level")
)

// Debug
func v(level int, format string, args ...interface{}) {
	if *vLevel < level {
		return
	}
	log.Printf("[debug] "+format, args...)
}

// Only download 10 at a time
var limit = make(chan bool, 10)

// Check 5 in either direction for other tiles
var expand = []int{1, 2}

func init() {
	for i := 0; i < cap(limit); i++ {
		limit <- true
	}
}

// Don't download something twice
var fetched = struct {
	tried map[string]bool
	found map[string]bool
	sync.Mutex
}{
	tried: make(map[string]bool, 10000),
	found: make(map[string]bool, 200),
}

var wg sync.WaitGroup

func name(coord1 int, dim1 string, coord2 int, dim2 string) string {
	return fmt.Sprintf("%d%s%d%s.png", coord1, dim1, coord2, dim2)
}

func download(coord1 int, dim1 string, coord2 int, dim2 string) {
	defer wg.Done()

	shouldTry := func(image string) bool {
		fetched.Lock()
		defer fetched.Unlock()

		if fetched.tried[image] {
			return false
		}

		fetched.tried[image] = true
		return true
	}

	found := func(image string) {
		fetched.Lock()
		defer fetched.Unlock()

		fetched.found[image] = true
	}

	// Get the image name
	image := name(coord1, dim1, coord2, dim2)
	path := filepath.Join(*dir, image)

	// Don't redownload an image that we've already tried
	if !shouldTry(image) {
		v(3, "shouldTry(%q) == false", image)
		return
	}

	// Does it exist already from a previous run?
	if _, err := os.Stat(path); err == nil {
		log.Printf("Existing %s...", image)
	} else if *local {
		// Do nothing...
		return
	} else {
		// Limit outgoing downloads (to be nice)
		<-limit
		defer func() { limit <- true }()

		// Download iamge
		resp, err := http.Get("http://imgs.xkcd.com/clickdrag/" + image)
		if err != nil {
			log.Printf("get(%q): %s", image, err)
			return
		}
		defer resp.Body.Close()

		// Make sure it's a 200
		if resp.StatusCode/100 != 2 {
			if resp.StatusCode != 404 {
				log.Printf("fetch(%q): %s", image, resp.Status)
			} else {
				v(1, "fetch(%q): %s", image, resp.Status)
			}
			return
		}
		log.Printf("Fetched %s...", image)

		// Open file
		file, err := os.Create(path)
		if err != nil {
			log.Printf("create(%q): %s", image, err)
			return
		}
		defer file.Close()

		// Write to file
		if _, err := io.Copy(file, resp.Body); err != nil {
			log.Printf("copy(%q): %s", image, err)
			return
		}
	}
	found(image)

	// Download ajacent images
	for _, d1 := range []int{-2, -1, 0, 1, 2} {
		for _, d2 := range []int{-2, -1, 0, 1, 2} {
			if d1 == 0 && d2 == 0 {
				continue
			}

			c1, c2 := coord1+d1, coord2+d2
			if c1 < 1 || c2 < 1 {
				continue
			}

			wg.Add(1)
			v(3, "%s: considering %d %s, %d %s", image, c1, dim1, c2, dim2)
			go download(c1, dim1, c2, dim2)
		}
	}
}

func status() {
	count := func() (a, f int) {
		fetched.Lock()
		defer fetched.Unlock()
		return len(fetched.tried), len(fetched.found)
	}

	for {
		a, f := count()
		log.Printf("Downloaded: %d, Attempted: %d, Pending: %d", f, a, runtime.NumGoroutine())
		time.Sleep(1 * time.Second)
	}
}

func main() {
	flag.Parse()

	if err := os.MkdirAll(*dir, 0755); err != nil {
		log.Fatalf("mkdir(%q): %s", *dir, err)
	}
	index, err := os.Create(filepath.Join(*dir, "index.html"))
	if err != nil {
		log.Fatalf("create(%q): %s", "index.html", err)
	}
	defer index.Close()

	// Starting points
	wg.Add(4)
	go download(1, "n", 1, "e")
	go download(1, "n", 1, "w")
	go download(3, "s", 7, "e")
	go download(15, "s", 1, "w")

	// Empties
	wg.Add(4)
	go download(11, "n", 11, "e")
	go download(11, "n", 11, "w")
	go download(11, "s", 11, "e")
	go download(11, "s", 11, "e")

	go status()
	wg.Wait()

	// Just to be paranoid...
	fetched.Lock()
	defer fetched.Unlock()

	// Boilerplate
	fmt.Fprintf(index, "<html><head>\n")
	defer fmt.Fprintf(index, "</html>\n")
	fmt.Fprintf(index, "<title>clickdrag</title>\n")
	fmt.Fprintf(index, "<style>*{margin:0;padding:0;border-collapse:collapse}</style>\n")
	fmt.Fprintf(index, "</head><body>\n")
	defer fmt.Fprintf(index, "</body>\n")
	fmt.Fprintf(index, "<table>\n")
	defer fmt.Fprintf(index, "</table>\n")

	abs := func(x int) int {
		if x < 0 {
			return -x
		}
		return x
	}

	// Image table
	for r := 50; r >= -50; r-- {
		var ns string
		switch {
		case r < 0:
			ns = "s"
		case r > 0:
			ns = "n"
		default:
			continue
		}

		fmt.Fprintf(index, "  <tr> <!-- row %d -->\n", r)
		for c := -50; c <= 50; c++ {
			var ew string
			switch {
			case c < 0:
				ew = "w"
			case c > 0:
				ew = "e"
			default:
				continue
			}

			fmt.Fprintf(index, "  <td>")

			if image := name(abs(r), ns, abs(c), ew); fetched.found[image] {
				fmt.Fprintf(index, "<img src=%q />", image)
			} else if fetched.tried[image] {
				v(2, "Could not find %q", image)
				fmt.Fprintf(index, "<img src=%q />", name(11, ns, 11, ew))
			} else {
				v(3, "Did not attempt %q", image)
				fmt.Fprintf(index, "&nbsp;")
			}

			fmt.Fprintf(index, "</td> <!-- %d (%s) %d (%s) -->\n", r, ns, c, ew)
		}
		fmt.Fprintf(index, "  </tr>\n")
	}

	log.Printf("Wrote index.html")
}
