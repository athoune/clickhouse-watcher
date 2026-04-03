// rrd.go
package rrd

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// Résolutions
const (
	ResolutionDay   = 2 * time.Minute
	ResolutionWeek  = 15 * time.Minute
	ResolutionMonth = 1 * time.Hour

	SlotsDay   = 720 // 24h / 2min
	SlotsWeek  = 672 // 7j  / 15min
	SlotsMonth = 744 // 31j / 1h
)

// Ring est un anneau circulaire de mesures horodatées.
// Stockage binaire compact : 2×8 octets par slot (timestamp int64 + valeur int64).
type Ring struct {
	timestamps []*int64 // nil = slot vide
	values     []int64
	size       int
	head       int // prochain slot à écrire
}

func newRing(size int) *Ring {
	return &Ring{
		timestamps: make([]*int64, size),
		values:     make([]int64, size),
		size:       size,
	}
}

// push insère une valeur. Écrase le slot le plus ancien si l'anneau est plein.
func (r *Ring) push(ts time.Time, value int64) {
	t := ts.Unix()
	r.timestamps[r.head] = &t
	r.values[r.head] = value
	r.head = (r.head + 1) % r.size
}

// Sample est un point de mesure retourné aux appelants.
type Sample struct {
	At    time.Time
	Value int64
}

// ReadAll retourne les échantillons du plus récent au plus ancien.
func (r *Ring) ReadAll() []Sample {
	out := make([]Sample, 0, r.size)
	// Parcourir en sens inverse pour avoir le plus récent en premier
	for i := r.size - 1; i >= 0; i-- {
		idx := (r.head + i) % r.size
		if r.timestamps[idx] == nil {
			continue
		}
		out = append(out, Sample{
			At:    time.Unix(*r.timestamps[idx], 0),
			Value: r.values[idx],
		})
	}
	return out
}

// RRD gère les 3 anneaux et leur agrégation automatique.
type RRD struct {
	mu   sync.RWMutex
	day  *Ring
	week *Ring
	mon  *Ring

	lastWeek  time.Time // horodatage du dernier agrégat 15min écrit
	lastMonth time.Time // horodatage du dernier agrégat 1h écrit

	path string // chemin du fichier de persistance (vide = pas de persistance)
}

func New(persistPath string) (*RRD, error) {
	rrd := &RRD{
		day:  newRing(SlotsDay),
		week: newRing(SlotsWeek),
		mon:  newRing(SlotsMonth),
		path: persistPath,
	}
	if persistPath != "" {
		if err := rrd.load(); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("rrd load: %w", err)
		}
	}
	return rrd, nil
}

// Record enregistre une nouvelle mesure au moment présent.
// À appeler toutes les 2 minutes.
func (rrd *RRD) Record(value int64) error {
	now := time.Now().Truncate(ResolutionDay)

	rrd.mu.Lock()
	defer rrd.mu.Unlock()

	// Anneau jour — toujours alimenté
	rrd.day.push(now, value)

	// Anneau semaine — agrégation toutes les 15 min (on prend la dernière valeur)
	if now.Sub(rrd.lastWeek) >= ResolutionWeek {
		rrd.week.push(now, value)
		rrd.lastWeek = now
	}

	// Anneau mois — agrégation toutes les heures
	if now.Sub(rrd.lastMonth) >= ResolutionMonth {
		rrd.mon.push(now, value)
		rrd.lastMonth = now
	}

	if rrd.path != "" {
		return rrd.save()
	}
	return nil
}

// QueryDay retourne les mesures des dernières 24h (résolution 2 min).
func (rrd *RRD) QueryDay() []Sample {
	rrd.mu.RLock()
	defer rrd.mu.RUnlock()
	return rrd.day.ReadAll()
}

// QueryWeek retourne les mesures des 7 derniers jours (résolution 15 min).
func (rrd *RRD) QueryWeek() []Sample {
	rrd.mu.RLock()
	defer rrd.mu.RUnlock()
	return rrd.week.ReadAll()
}

// QueryMonth retourne les mesures du dernier mois (résolution 1h).
func (rrd *RRD) QueryMonth() []Sample {
	rrd.mu.RLock()
	defer rrd.mu.RUnlock()
	return rrd.mon.ReadAll()
}
