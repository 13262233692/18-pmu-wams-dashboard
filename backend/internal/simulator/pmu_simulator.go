package simulator

import (
	"math"
	"math/rand"
	"sync"
	"time"
	"wams-dashboard/internal/models"
)

type PMUSimulator struct {
	pmuCount    int
	stopChan    chan struct{}
	running     bool
	mu          sync.Mutex
	seqNum      uint32
	stationBase []struct {
		name      string
		baseAngle float64
		baseMag   float64
		angleVel  float64
	}
}

func NewPMUSimulator(pmuCount int) *PMUSimulator {
	sim := &PMUSimulator{
		pmuCount: pmuCount,
		stopChan: make(chan struct{}),
	}

	stations := []string{
		"华东-换流站A", "华北-变电站B", "华中-变电站C",
		"西北-变电站D", "西南-换流站E", "华南-变电站F",
		"东北-变电站G", "山东-变电站H",
	}

	baseAngles := []float64{0, 15, -10, 25, -20, 8, -25, 18}
	baseMags := []float64{525.0, 518.0, 522.0, 520.0, 515.0, 523.0, 519.0, 521.0}

	for i := 0; i < pmuCount && i < len(stations); i++ {
		sim.stationBase = append(sim.stationBase, struct {
			name      string
			baseAngle float64
			baseMag   float64
			angleVel  float64
		}{
			name:      stations[i],
			baseAngle: baseAngles[i],
			baseMag:   baseMags[i],
			angleVel:  0.001 + rand.Float64()*0.002,
		})
	}

	return sim
}

func (s *PMUSimulator) Start(outChan chan<- *models.PhasorMeasurement) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	startTime := time.Now()

	for {
		select {
		case <-s.stopChan:
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
			return
		case t := <-ticker.C:
			elapsed := t.Sub(startTime).Seconds()

			for i, base := range s.stationBase {
				pm := s.generatePMUMeasurement(i, base, elapsed, t)
				select {
				case outChan <- pm:
				default:
				}
			}
		}
	}
}

func (s *PMUSimulator) generatePMUMeasurement(pmuIdx int, base struct {
	name      string
	baseAngle float64
	baseMag   float64
	angleVel  float64
}, elapsed float64, t time.Time) *models.PhasorMeasurement {
	s.mu.Lock()
	s.seqNum++
	seqNum := s.seqNum
	s.mu.Unlock()

	baseAngleRad := base.baseAngle * math.Pi / 180.0
	angleOscillation := math.Sin(elapsed*0.5+float64(pmuIdx)) * 0.008
	angleNoise := (rand.Float64() - 0.5) * 0.002
	currentAngle := baseAngleRad + angleOscillation + angleNoise + base.angleVel*elapsed

	magOscillation := math.Sin(elapsed*0.3+float64(pmuIdx)*0.5) * 2.0
	magNoise := (rand.Float64() - 0.5) * 0.8
	currentMag := base.baseMag + magOscillation + magNoise

	spike := 0.0
	if rand.Float64() < 0.003 {
		spike = (rand.Float64() - 0.5) * 15.0
	}
	currentMag += spike

	phaseVoltages := s.generateThreePhase(currentMag, currentAngle)
	phaseCurrents := s.generateThreePhase(currentMag*0.3, currentAngle-0.35)

	a := complex(-0.5, math.Sqrt(3)/2)
	a2 := complex(-0.5, -math.Sqrt(3)/2)
	vA := complex(phaseVoltages[0].Real, phaseVoltages[0].Imag)
	vB := complex(phaseVoltages[1].Real, phaseVoltages[1].Imag)
	vC := complex(phaseVoltages[2].Real, phaseVoltages[2].Imag)
	v1 := (vA + a*vB + a2*vC) / complex(3.0, 0)

	iA := complex(phaseCurrents[0].Real, phaseCurrents[0].Imag)
	iB := complex(phaseCurrents[1].Real, phaseCurrents[1].Imag)
	iC := complex(phaseCurrents[2].Real, phaseCurrents[2].Imag)
	i1 := (iA + a*iB + a2*iC) / complex(3.0, 0)

	freqOscillation := math.Sin(elapsed*0.2) * 0.015
	freqNoise := (rand.Float64() - 0.5) * 0.005
	frequency := 50.0 + freqOscillation + freqNoise

	rocof := -freqOscillation * 0.2 * 10
	if rand.Float64() < 0.002 {
		rocof += (rand.Float64() - 0.5) * 2.0
	}

	return &models.PhasorMeasurement{
		PMUID:        base.name,
		StationName:  base.name,
		Timestamp:    t,
		UnixNano:     t.UnixNano(),
		SequenceNum:  seqNum,
		PhaseVoltage: phaseVoltages,
		PhaseCurrent: phaseCurrents,
		PositiveSeqV: models.ComplexPhasor{
			Real:      real(v1),
			Imag:      imag(v1),
			Magnitude: math.Sqrt(real(v1)*real(v1) + imag(v1)*imag(v1)),
			Angle:     math.Atan2(imag(v1), real(v1)),
		},
		PositiveSeqI: models.ComplexPhasor{
			Real:      real(i1),
			Imag:      imag(i1),
			Magnitude: math.Sqrt(real(i1)*real(i1) + imag(i1)*imag(i1)),
			Angle:     math.Atan2(imag(i1), real(i1)),
		},
		Frequency: frequency,
		ROCOF:     rocof,
		Status:    0,
		Filtered:  false,
	}
}

func (s *PMUSimulator) generateThreePhase(baseMag, baseAngle float64) [3]models.ComplexPhasor {
	var result [3]models.ComplexPhasor
	phaseShifts := [3]float64{0, -2 * math.Pi / 3, 2 * math.Pi / 3}

	for i := 0; i < 3; i++ {
		angle := baseAngle + phaseShifts[i]
		mag := baseMag * (0.98 + rand.Float64()*0.04)

		realPart := mag * math.Cos(angle)
		imagPart := mag * math.Sin(angle)

		result[i] = models.ComplexPhasor{
			Real:      realPart,
			Imag:      imagPart,
			Magnitude: mag,
			Angle:     angle,
		}
	}

	return result
}

func (s *PMUSimulator) Stop() {
	s.mu.Lock()
	if s.running {
		close(s.stopChan)
		s.running = false
	}
	s.mu.Unlock()
}
