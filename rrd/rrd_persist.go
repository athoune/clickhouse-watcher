// rrd_persist.go
package rrd

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"time"
)

// Format binaire du fichier :
//
//	[magic 4B][version 1B]
//	[lastWeekUnix 8B][lastMonthUnix 8B]
//	pour chaque anneau : [head 4B][size 4B][slots : timestamp 8B | value 8B]
//	  (timestamp == math.MinInt64 → slot vide)
const (
	magic             = "RRD1"
	emptySlotSentinel = int64(-1 << 63) // math.MinInt64
)

func (rrd *RRD) save() error {
	tmp := rrd.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := newBinWriter(f)
	w.writeBytes([]byte(magic))
	w.writeInt64(rrd.lastWeek.Unix())
	w.writeInt64(rrd.lastMonth.Unix())
	for _, r := range []*Ring{rrd.day, rrd.week, rrd.mon} {
		w.writeInt32(int32(r.head))
		w.writeInt32(int32(r.size))
		for i := 0; i < r.size; i++ {
			if r.timestamps[i] == nil {
				w.writeInt64(emptySlotSentinel)
				w.writeInt64(0)
			} else {
				w.writeInt64(*r.timestamps[i])
				w.writeInt64(r.values[i])
			}
		}
	}
	if err := w.err; err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	f.Close()
	return os.Rename(tmp, rrd.path)
}

func (rrd *RRD) load() error {
	f, err := os.Open(rrd.path)
	if err != nil {
		return err
	}
	defer f.Close()

	r := newBinReader(f)
	m := make([]byte, 4)
	r.readBytes(m)
	if string(m) != magic {
		return fmt.Errorf("fichier RRD invalide")
	}
	rrd.lastWeek = unixToTime(r.readInt64())
	rrd.lastMonth = unixToTime(r.readInt64())

	for _, ring := range []*Ring{rrd.day, rrd.week, rrd.mon} {
		head := int(r.readInt32())
		size := int(r.readInt32())
		if size != ring.size {
			return fmt.Errorf("taille d'anneau incompatible : %d != %d", size, ring.size)
		}
		ring.head = head
		for i := 0; i < size; i++ {
			ts := r.readInt64()
			val := r.readInt64()
			if ts == emptySlotSentinel {
				ring.timestamps[i] = nil
			} else {
				t := ts
				ring.timestamps[i] = &t
				ring.values[i] = val
			}
		}
	}
	return r.err
}

func unixToTime(u int64) time.Time {
	if u <= 0 {
		return time.Time{}
	}
	return time.Unix(u, 0)
}

// --- little-endian binary read/write helpers ---

type binWriter struct {
	w   io.Writer
	err error
}

func newBinWriter(w io.Writer) *binWriter { return &binWriter{w: w} }
func (b *binWriter) writeBytes(v []byte) {
	if b.err == nil {
		_, b.err = b.w.Write(v)
	}
}
func (b *binWriter) writeInt64(v int64) {
	if b.err == nil {
		b.err = binary.Write(b.w, binary.LittleEndian, v)
	}
}
func (b *binWriter) writeInt32(v int32) {
	if b.err == nil {
		b.err = binary.Write(b.w, binary.LittleEndian, v)
	}
}

type binReader struct {
	r   io.Reader
	err error
}

func newBinReader(r io.Reader) *binReader { return &binReader{r: r} }
func (b *binReader) readBytes(v []byte) {
	if b.err == nil {
		_, b.err = io.ReadFull(b.r, v)
	}
}
func (b *binReader) readInt64() int64 {
	var v int64
	if b.err == nil {
		b.err = binary.Read(b.r, binary.LittleEndian, &v)
	}
	return v
}
func (b *binReader) readInt32() int32 {
	var v int32
	if b.err == nil {
		b.err = binary.Read(b.r, binary.LittleEndian, &v)
	}
	return v
}
