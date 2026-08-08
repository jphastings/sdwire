package blockdev

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestPackLocationID(t *testing.T) {
	cases := []struct {
		bus   int
		ports []int
		want  uint32
	}{
		{1, []int{1, 1, 3}, 0x01113000},
		{2, []int{3, 1, 4, 2}, 0x02314200},
		{1, nil, 0x01000000},
	}
	for _, c := range cases {
		if got := packLocationID(c.bus, c.ports); got != c.want {
			t.Errorf("packLocationID(%d, %v) = %#08x, want %#08x", c.bus, c.ports, got, c.want)
		}
	}
}

func TestBSDNameFromPath(t *testing.T) {
	if got := bsdNameFromPath("/dev/disk4"); got != "disk4" {
		t.Errorf("bsdNameFromPath = %q, want disk4", got)
	}
}

// targetReaderRef identifies the SD reader captured in testdata/ioreg_*.xml:
// bus 1, port path [1,1,3] -> locationID 0x01113000, a Realtek
// (vendor 0x0BDA) USB3.0-CRW (product 0x0316).
var targetReaderRef = Ref{Vendor: 0x0BDA, Product: 0x0316, Bus: 1, PortPath: []int{1, 1, 3}}

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("reading testdata/%s: %v", name, err)
	}
	return data
}

func TestFindDarwinMatchesReaderByLocationIgnoringDecoys(t *testing.T) {
	bsdName, size, err := findDarwin(readTestdata(t, "ioreg_found.xml"), targetReaderRef)
	if err != nil {
		t.Fatalf("findDarwin: %v", err)
	}
	if bsdName != "disk4" {
		t.Errorf("bsdName = %q, want disk4", bsdName)
	}
	if size != 15931539456 {
		t.Errorf("size = %d, want 15931539456", size)
	}
}

func TestFindDarwinNoMatchReturnsErrNotFound(t *testing.T) {
	_, _, err := findDarwin(readTestdata(t, "ioreg_none.xml"), targetReaderRef)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestFindDarwinWrongPortReturnsErrNotFound(t *testing.T) {
	ref := targetReaderRef
	ref.PortPath = []int{1, 1, 9}
	_, _, err := findDarwin(readTestdata(t, "ioreg_found.xml"), ref)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestFindDarwinMultipleWholeDisksReturnsErrAmbiguous(t *testing.T) {
	_, _, err := findDarwin(readTestdata(t, "ioreg_ambiguous.xml"), targetReaderRef)
	if !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("err = %v, want ErrAmbiguous", err)
	}
	if got := err.Error(); got == "" || !strings.Contains(got, "disk4") || !strings.Contains(got, "disk6") {
		t.Errorf("error message = %q, want it to list disk4 and disk6", got)
	}
}

func TestSizeForBSDName(t *testing.T) {
	size, err := sizeForBSDName(readTestdata(t, "ioreg_found.xml"), "disk5")
	if err != nil {
		t.Fatalf("sizeForBSDName: %v", err)
	}
	if size != 15931539456 {
		t.Errorf("size = %d, want 15931539456", size)
	}
}

func TestSizeForBSDNamePartition(t *testing.T) {
	size, err := sizeForBSDName(readTestdata(t, "ioreg_found.xml"), "disk4s1")
	if err != nil {
		t.Fatalf("sizeForBSDName: %v", err)
	}
	if size != 268435456 {
		t.Errorf("size = %d, want 268435456", size)
	}
}

func TestSizeForBSDNameNotFound(t *testing.T) {
	if _, err := sizeForBSDName(readTestdata(t, "ioreg_found.xml"), "disk99"); err == nil {
		t.Fatal("expected an error for an unknown BSD name")
	}
}
