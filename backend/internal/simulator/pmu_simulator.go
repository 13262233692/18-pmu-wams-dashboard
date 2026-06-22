package simulator

import (
	"log"
	"math"
	"math/rand"
	"sync"
	"time"
	"wams-dashboard/internal/models"
)

type OscInjectionConfig struct {
	Enabled       bool
	FrequencyHz   float64
	DampingRatio  float64
	Amplitude     float64
	StartAtSec    float64
	DurationSec   float64
	TargetStation string
	InjectAngle   bool
	InjectPower   bool
}

type PMUSimulator struct {
	pmuCount    int
	stopChan    chan struct{}
	running     bool
	mu          sync.Mutex
	seqNum      uint32
	stationBase []struct {
		name        string
		baseAngle   float64
		baseMag     float64
		basePowerMW float64
		angleVel    float64
	}

	oscInjections []OscInjectionConfig
	oscMu         sync.RWMutex

	elapsedTime float64
	timeMu      sync.RWMutex
}

func NewPMUSimulator(pmuCount int) *PMUSimulator {
	sim := &PMUSimulator{
		pmuCount: pmuCount,
		stopChan: make(chan struct{}),
	}

	stations := []string{
		"华东-换流站A", "华北-变电站B", "华中-枢纽站C",
		"西南-水电厂D", "西北-火电厂E", "东北-风电场F",
		"华南-核电G", "山东-光伏H",
	}

	baseAngles := []float64{0, 15, -10, 25, -20, 8, -25, 18}
	baseMags := []float64{525.0, 518.0, 522.0, 520.0, 515.0, 523.0, 519.0, 521.0}
	basePowers := []float64{2800, 1500, 2200, 3500, 2400, 700, 3200, 900}

	for i := 0; i < pmuCount && i < len(stations); i++ {
		sim.stationBase = append(sim.stationBase, struct {
			name        string
			baseAngle   float64
			baseMag     float64
			basePowerMW float64
			angleVel    float64
		}{
			name:        stations[i],
			baseAngle:   baseAngles[i],
			baseMag:     baseMags[i],
			basePowerMW: basePowers[i],
			angleVel:    0.001 + rand.Float64()*0.002,
		})
	}

	sim.oscInjections = []OscInjectionConfig{
		{
			Enabled:       true,
			FrequencyHz:   0.8,
			DampingRatio:  -0.03,
			Amplitude:     250,
			StartAtSec:    60,
			DurationSec:   30,
			TargetStation: "华东-换流站A",
			InjectAngle:   true,
			InjectPower:   true,
		},
		{
			Enabled:       true,
			FrequencyHz:   1.2,
			DampingRatio:  -0.02,
			Amplitude:     180,
			StartAtSec:    150,
			DurationSec:   25,
			TargetStation: "华中-枢纽站C",
			InjectAngle:   true,
			InjectPower:   true,
		},
		{
			Enabled:       true,
			FrequencyHz:   0.4,
			DampingRatio:  -0.015,
			Amplitude:     120,
			StartAtSec:    250,
			DurationSec:   40,
			TargetStation: "西南-水电厂D",
			InjectAngle:   true,
			InjectPower:   true,
		},
	}

	return sim
}

func (s *PMUSimulator) AddOscillationInjection(cfg OscInjectionConfig) {
	s.oscMu.Lock()
	defer s.oscMu.Unlock()
	s.oscInjections = append(s.oscInjections, cfg)
}

func (s *PMUSimulator) ClearOscillationInjections() {
	s.oscMu.Lock()
	defer s.oscMu.Unlock()
	s.oscInjections = s.oscInjections[:0]
}

func (s *PMUSimulator) GetElapsedTime() float64 {
	s.timeMu.RLock()
	defer s.timeMu.RUnlock()
	return s.elapsedTime
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
	lastLog := 0

	for {
		select {
		case <-s.stopChan:
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
			return
		case t := <-ticker.C:
			elapsed := t.Sub(startTime).Seconds()

			s.timeMu.Lock()
			s.elapsedTime = elapsed
			s.timeMu.Unlock()

			if int(elapsed) > lastLog && int(elapsed)%20 == 0 {
				lastLog = int(elapsed)
				s.checkOscillationStatus(elapsed)
			}

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

func (s *PMUSimulator) checkOscillationStatus(elapsed float64) {
	s.oscMu.RLock()
	defer s.oscMu.RUnlock()

	activeCount := 0
	for _, inj := range s.oscInjections {
		if inj.Enabled && elapsed >= inj.StartAtSec && elapsed < inj.StartAtSec+inj.DurationSec {
			activeCount++
			growth := math.Exp(-inj.DampingRatio * 2 * math.Pi * inj.FrequencyHz * (elapsed - inj.StartAtSec))
			log.Printf("[SIM-OSC] Active injection @ %s: f=%.2fHz, ζ=%.4f, growth=%.2fx, t=%.0fs",
				inj.TargetStation, inj.FrequencyHz, inj.DampingRatio, growth, elapsed)
		}
	}
	if activeCount > 0 {
		log.Printf("[SIM-OSC] %d active oscillation injections at t=%.0fs", activeCount, elapsed)
	}
}

func (s *PMUSimulator) generatePMUMeasurement(pmuIdx int, base struct {
	name        string
	baseAngle   float64
	baseMag     float64
	basePowerMW float64
	angleVel    float64
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

	basePowerMW := base.basePowerMW
	powerNoise := (rand.Float64() - 0.5) * basePowerMW * 0.02
	powerLoadOsc := math.Sin(elapsed*0.1+float64(pmuIdx)*1.2) * basePowerMW * 0.03
	activePowerMW := basePowerMW + powerNoise + powerLoadOsc

	injectedAngleDeg := 0.0
	injectedPowerMW := 0.0

	s.oscMu.RLock()
	injections := s.oscInjections
	s.oscMu.RUnlock()

	for _, inj := range injections {
		if !inj.Enabled {
			continue
		}
		if inj.TargetStation != base.name {
			continue
		}
		if elapsed < inj.StartAtSec || elapsed >= inj.StartAtSec+inj.DurationSec {
			continue
		}

		tInOsc := elapsed - inj.StartAtSec
		sigma := -inj.DampingRatio * 2 * math.Pi * inj.FrequencyHz
		envelope := math.Exp(sigma * tInOsc)
		oscPhase := 2 * math.Pi * inj.FrequencyHz * tInOsc

		oscValue := inj.Amplitude * envelope * math.Sin(oscPhase)

		if inj.InjectPower {
			injectedPowerMW += oscValue
		}
		if inj.InjectAngle {
			angleFactor := 1.0 / 50.0
			injectedAngleDeg += oscValue * angleFactor
		}
	}

	activePowerMW += injectedPowerMW
	currentAngle += injectedAngleDeg * math.Pi / 180.0

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

	powerAngle := currentAngle - (currentAngle - 0.35)
	reactivePowerMW := math.Abs(basePowerMW * math.Tan(powerAngle) * 0.3)

	freqOscillation := math.Sin(elapsed*0.2) * 0.015
	freqInjection := injectedPowerMW / basePowerMW * 0.05
	freqNoise := (rand.Float64() - 0.5) * 0.005
	frequency := 50.0 + freqOscillation + freqNoise + freqInjection

	rocof := -freqOscillation * 0.2 * 10
	if math.Abs(freqInjection) > 1e-6 {
		rocof += -freqInjection * 0.2 * 10
	}
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
		Frequency:     frequency,
		ROCOF:         rocof,
		Status:        0,
		Filtered:      false,
		ActivePower:   activePowerMW,
		ReactivePower: reactivePowerMW,
		VoltageAngle:  currentAngle * 180 / math.Pi,
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
