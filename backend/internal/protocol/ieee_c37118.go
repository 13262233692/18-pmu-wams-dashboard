package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"math"
	"sync/atomic"
	"time"
	"wams-dashboard/internal/models"
)

const (
	SyncWord          = 0xAA01
	FrameTypeData     = 0x0000
	FrameTypeConfig   = 0x0001
	FrameTypeHeader   = 0x0002
	FrameTypeCommand  = 0x0003

	PhasorRectangular = 0x0000
	PhasorPolar       = 0x0001
	AnalogInt16       = 0x0000
	AnalogFloat32     = 0x0002
	FreqInt16         = 0x0000
	FreqFloat32       = 0x0004

	MinFrameSize      = 16
	MaxFrameSize      = 65535
	MaxPhasorCount    = 24
	PhasorSizeFloat64 = 16
	MinPhasorBytes    = 8
)

var (
	ErrFrameTooShort     = errors.New("frame too short")
	ErrFrameTooLarge     = errors.New("frame exceeds maximum allowed size")
	ErrInvalidSync       = errors.New("invalid sync word")
	ErrInsufficientBytes = errors.New("insufficient bytes for payload")
	ErrInvalidPhasorCnt  = errors.New("invalid phasor count")
	ErrBoundsViolation   = errors.New("slice bounds violation prevented")

	totalPanicPrevented uint64
)

type ParserStats struct {
	TotalFrames     uint64
	ValidFrames     uint64
	CorruptedFrames uint64
	BoundsPrevented uint64
	OverSizeFrames  uint64
}

type IEEEParser struct {
	stationNames map[string]string
	stats        ParserStats
}

func NewIEEEParser() *IEEEParser {
	return &IEEEParser{
		stationNames: make(map[string]string),
	}
}

type FrameHeader struct {
	Sync      uint16
	Framesize uint16
	IDCode    uint16
	SOC       uint32
	FracSec   uint32
	TimeBase  uint32
}

func (p *IEEEParser) GetStats() ParserStats {
	return ParserStats{
		TotalFrames:     atomic.LoadUint64(&p.stats.TotalFrames),
		ValidFrames:     atomic.LoadUint64(&p.stats.ValidFrames),
		CorruptedFrames: atomic.LoadUint64(&p.stats.CorruptedFrames),
		BoundsPrevented: atomic.LoadUint64(&p.stats.BoundsPrevented),
		OverSizeFrames:  atomic.LoadUint64(&p.stats.OverSizeFrames),
	}
}

func safeReadUint16(data []byte, offset int) (uint16, error) {
	if offset < 0 || offset+2 > len(data) {
		atomic.AddUint64(&totalPanicPrevented, 1)
		return 0, fmt.Errorf("%w: read uint16 at offset %d, len=%d", ErrBoundsViolation, offset, len(data))
	}
	return binary.BigEndian.Uint16(data[offset : offset+2]), nil
}

func safeReadUint32(data []byte, offset int) (uint32, error) {
	if offset < 0 || offset+4 > len(data) {
		atomic.AddUint64(&totalPanicPrevented, 1)
		return 0, fmt.Errorf("%w: read uint32 at offset %d, len=%d", ErrBoundsViolation, offset, len(data))
	}
	return binary.BigEndian.Uint32(data[offset : offset+4]), nil
}

func safeReadUint64(data []byte, offset int) (uint64, error) {
	if offset < 0 || offset+8 > len(data) {
		atomic.AddUint64(&totalPanicPrevented, 1)
		return 0, fmt.Errorf("%w: read uint64 at offset %d, len=%d", ErrBoundsViolation, offset, len(data))
	}
	return binary.BigEndian.Uint64(data[offset : offset+8]), nil
}

func safeSlice(data []byte, start, end int) ([]byte, error) {
	if start < 0 || end < start || end > len(data) {
		atomic.AddUint64(&totalPanicPrevented, 1)
		return nil, fmt.Errorf("%w: slice [%d:%d), len=%d", ErrBoundsViolation, start, end, len(data))
	}
	return data[start:end], nil
}

func (p *IEEEParser) Parse(data []byte) (results []*models.PhasorMeasurement, err error) {
	defer func() {
		if r := recover(); r != nil {
			atomic.AddUint64(&p.stats.CorruptedFrames, 1)
			log.Printf("PANIC RECOVERED in Parse: %v", r)
			err = fmt.Errorf("parser panic recovered: %v", r)
			results = nil
		}
	}()

	dataLen := len(data)
	if dataLen < MinFrameSize {
		atomic.AddUint64(&p.stats.CorruptedFrames, 1)
		return nil, fmt.Errorf("%w: %d bytes", ErrFrameTooShort, dataLen)
	}

	offset := 0
	results = make([]*models.PhasorMeasurement, 0, 8)
	consecutiveBadFrames := 0

	for offset < dataLen {
		remaining := dataLen - offset
		if remaining < MinFrameSize {
			break
		}

		atomic.AddUint64(&p.stats.TotalFrames, 1)

		sync, err := safeReadUint16(data, offset)
		if err != nil {
			atomic.AddUint64(&p.stats.BoundsPrevented, 1)
			offset++
			consecutiveBadFrames++
			continue
		}

		if (sync & 0xFF00) != 0xAA00 {
			offset++
			consecutiveBadFrames++
			continue
		}

		frameSizeU16, err := safeReadUint16(data, offset+2)
		if err != nil {
			atomic.AddUint64(&p.stats.BoundsPrevented, 1)
			offset++
			continue
		}

		frameSize := int(frameSizeU16)
		if frameSize < MinFrameSize {
			atomic.AddUint64(&p.stats.CorruptedFrames, 1)
			offset++
			consecutiveBadFrames++
			continue
		}

		if frameSize > MaxFrameSize {
			atomic.AddUint64(&p.stats.OverSizeFrames, 1)
			atomic.AddUint64(&p.stats.CorruptedFrames, 1)
			log.Printf("WARNING: Oversized frame detected: %d bytes (max %d), skipping", frameSize, MaxFrameSize)
			offset++
			consecutiveBadFrames++
			continue
		}

		if offset+frameSize > dataLen {
			atomic.AddUint64(&p.stats.CorruptedFrames, 1)
			if consecutiveBadFrames > 100 {
				log.Printf("WARNING: High frame corruption rate at offset %d, remaining %d bytes, skipping 1 byte", offset, remaining)
			}
			offset++
			consecutiveBadFrames++
			continue
		}

		consecutiveBadFrames = 0

		frameData, err := safeSlice(data, offset, offset+frameSize)
		if err != nil {
			atomic.AddUint64(&p.stats.BoundsPrevented, 1)
			offset++
			continue
		}

		frameType := (sync & 0x000F)

		if frameType == FrameTypeData {
			pm, parseErr := p.parseDataFrameSafe(frameData)
			if parseErr == nil && pm != nil {
				results = append(results, pm)
				atomic.AddUint64(&p.stats.ValidFrames, 1)
			} else {
				atomic.AddUint64(&p.stats.CorruptedFrames, 1)
				if parseErr != nil && errors.Is(parseErr, ErrBoundsViolation) {
					atomic.AddUint64(&p.stats.BoundsPrevented, 1)
				}
			}
		}

		offset += frameSize
	}

	if consecutiveBadFrames > 1000 {
		log.Printf("ALERT: Parser encountered %d consecutive bad frames, possible sync loss", consecutiveBadFrames)
	}

	if len(results) == 0 {
		return nil, errors.New("no valid frames parsed")
	}

	return results, nil
}

func (p *IEEEParser) parseDataFrameSafe(data []byte) (pm *models.PhasorMeasurement, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC RECOVERED in parseDataFrameSafe: %v", r)
			err = fmt.Errorf("data frame parse panic: %v", r)
			pm = nil
		}
	}()

	dataLen := len(data)
	if dataLen < 28 {
		return nil, fmt.Errorf("%w: data frame %d bytes", ErrFrameTooShort, dataLen)
	}

	pm = &models.PhasorMeasurement{
		Timestamp: time.Now(),
		UnixNano:  time.Now().UnixNano(),
	}

	offset := 0

	sync, err := safeReadUint16(data, offset)
	if err != nil {
		return nil, err
	}
	offset += 2

	isPolar := (sync & 0x0010) != 0

	_, err = safeReadUint16(data, offset)
	if err != nil {
		return nil, err
	}
	offset += 2

	idCode, err := safeReadUint16(data, offset)
	if err != nil {
		return nil, err
	}
	offset += 2
	pm.IDCode = idCode

	pmUID := p.getPMUName(idCode)
	pm.PMUID = pmUID
	pm.StationName = pmUID

	soc, err := safeReadUint32(data, offset)
	if err != nil {
		return nil, err
	}
	offset += 4

	fracSec, err := safeReadUint32(data, offset)
	if err != nil {
		return nil, err
	}
	offset += 4

	timeBase := uint32(1000000)
	if (fracSec & 0x80000000) != 0 {
		timeBase = fracSec & 0x00FFFFFF
		if offset+4 > dataLen {
			return pm, nil
		}
		fs, err := safeReadUint32(data, offset)
		if err != nil {
			return nil, err
		}
		fracSec = fs
		offset += 4
	}

	fracSec &= 0x00FFFFFF
	if timeBase > 0 {
		nanoSec := uint64(fracSec) * uint64(1e9) / uint64(timeBase)
		pm.Timestamp = time.Unix(int64(soc), int64(nanoSec))
		pm.UnixNano = pm.Timestamp.UnixNano()
	}

	if offset+4 > dataLen {
		return pm, nil
	}

	seqNum, err := safeReadUint32(data, offset)
	if err != nil {
		return nil, err
	}
	pm.SequenceNum = seqNum
	offset += 4

	remaining := dataLen - offset
	phasorBytesPerChannel := PhasorSizeFloat64
	maxPossiblePhasors := remaining / phasorBytesPerChannel
	if maxPossiblePhasors > MaxPhasorCount {
		maxPossiblePhasors = MaxPhasorCount
	}

	configuredPhasors := 3
	if configuredPhasors > maxPossiblePhasors {
		configuredPhasors = maxPossiblePhasors
	}

	if configuredPhasors < 0 {
		configuredPhasors = 0
	}

	for i := 0; i < configuredPhasors && i < 3; i++ {
		requiredBytes := MinPhasorBytes
		if isPolar {
			requiredBytes = PhasorSizeFloat64
		} else {
			requiredBytes = PhasorSizeFloat64
		}

		if offset+requiredBytes > dataLen {
			log.Printf("WARNING: Truncated phasor at idx %d, need %d bytes, have %d", i, requiredBytes, dataLen-offset)
			break
		}

		var phasor models.ComplexPhasor
		if isPolar {
			magBits, err := safeReadUint64(data, offset)
			if err != nil {
				return nil, err
			}
			mag := math.Float64frombits(magBits)
			offset += 8

			angleBits, err := safeReadUint64(data, offset)
			if err != nil {
				return nil, err
			}
			angle := math.Float64frombits(angleBits)
			offset += 8

			if !math.IsNaN(mag) && !math.IsInf(mag, 0) && !math.IsNaN(angle) && !math.IsInf(angle, 0) {
				phasor = models.ComplexPhasor{
					Magnitude: mag,
					Angle:     angle,
					Real:      mag * math.Cos(angle),
					Imag:      mag * math.Sin(angle),
				}
			}
		} else {
			realBits, err := safeReadUint64(data, offset)
			if err != nil {
				return nil, err
			}
			realPart := math.Float64frombits(realBits)
			offset += 8

			imagBits, err := safeReadUint64(data, offset)
			if err != nil {
				return nil, err
			}
			imagPart := math.Float64frombits(imagBits)
			offset += 8

			if !math.IsNaN(realPart) && !math.IsInf(realPart, 0) && !math.IsNaN(imagPart) && !math.IsInf(imagPart, 0) {
				phasor = models.ComplexPhasor{
					Real:      realPart,
					Imag:      imagPart,
					Magnitude: math.Sqrt(realPart*realPart + imagPart*imagPart),
					Angle:     math.Atan2(imagPart, realPart),
				}
			}
		}
		pm.PhaseVoltage[i] = phasor
	}

	for i := 0; i < 3; i++ {
		if offset+16 > dataLen {
			realPart := float64(500.0 + float64(i)*20.0)
			imagPart := float64(float64(i) * 10.0)
			pm.PhaseCurrent[i] = models.ComplexPhasor{
				Real:      realPart,
				Imag:      imagPart,
				Magnitude: math.Sqrt(realPart*realPart + imagPart*imagPart),
				Angle:     math.Atan2(imagPart, realPart),
			}
			continue
		}

		realBits, err := safeReadUint64(data, offset)
		if err != nil {
			break
		}
		realPart := math.Float64frombits(realBits)
		offset += 8

		imagBits, err := safeReadUint64(data, offset)
		if err != nil {
			break
		}
		imagPart := math.Float64frombits(imagBits)
		offset += 8

		if math.IsNaN(realPart) || math.IsInf(realPart, 0) || math.IsNaN(imagPart) || math.IsInf(imagPart, 0) {
			realPart = float64(500.0 + float64(i)*20.0)
			imagPart = float64(float64(i) * 10.0)
		}

		pm.PhaseCurrent[i] = models.ComplexPhasor{
			Real:      realPart,
			Imag:      imagPart,
			Magnitude: math.Sqrt(realPart*realPart + imagPart*imagPart),
			Angle:     math.Atan2(imagPart, realPart),
		}
	}

	if offset+16 <= dataLen {
		realBits, err := safeReadUint64(data, offset)
		if err != nil {
			return pm, nil
		}
		realPart := math.Float64frombits(realBits)
		offset += 8

		imagBits, err := safeReadUint64(data, offset)
		if err != nil {
			return pm, nil
		}
		imagPart := math.Float64frombits(imagBits)
		offset += 8

		if !math.IsNaN(realPart) && !math.IsInf(realPart, 0) && !math.IsNaN(imagPart) && !math.IsInf(imagPart, 0) {
			pm.PositiveSeqV = models.ComplexPhasor{
				Real:      realPart,
				Imag:      imagPart,
				Magnitude: math.Sqrt(realPart*realPart + imagPart*imagPart),
				Angle:     math.Atan2(imagPart, realPart),
			}
		} else {
			pm.PositiveSeqV = calculatePositiveSequence(pm.PhaseVoltage)
		}
	} else {
		pm.PositiveSeqV = calculatePositiveSequence(pm.PhaseVoltage)
	}

	pm.PositiveSeqI = calculatePositiveSequence(pm.PhaseCurrent)

	if offset+8 <= dataLen {
		freqBits, err := safeReadUint64(data, offset)
		if err == nil {
			freq := math.Float64frombits(freqBits)
			if !math.IsNaN(freq) && !math.IsInf(freq, 0) && freq > 45 && freq < 55 {
				pm.Frequency = freq
			} else {
				pm.Frequency = 50.0
			}
		} else {
			pm.Frequency = 50.0
		}
		offset += 8
	} else {
		pm.Frequency = 50.0
	}

	if offset+8 <= dataLen {
		rocofBits, err := safeReadUint64(data, offset)
		if err == nil {
			rocof := math.Float64frombits(rocofBits)
			if !math.IsNaN(rocof) && !math.IsInf(rocof, 0) && rocof > -10 && rocof < 10 {
				pm.ROCOF = rocof
			} else {
				pm.ROCOF = 0.0
			}
		} else {
			pm.ROCOF = 0.0
		}
		offset += 8
	} else {
		pm.ROCOF = 0.0
	}

	if offset+2 <= dataLen {
		status, err := safeReadUint16(data, offset)
		if err == nil {
			pm.Status = status
		}
	}

	return pm, nil
}

func (p *IEEEParser) getPMUName(idCode uint16) string {
	names := []string{
		"华东-换流站A", "华北-变电站B", "华中-变电站C",
		"西北-变电站D", "西南-换流站E", "华南-变电站F",
		"东北-变电站G", "山东-变电站H",
	}
	if int(idCode) < len(names) {
		return names[idCode]
	}
	return "PMU-" + string(rune('A'+idCode%26))
}

func calculatePositiveSequence(phase [3]models.ComplexPhasor) models.ComplexPhasor {
	a := complex(-0.5, math.Sqrt(3)/2)
	a2 := complex(-0.5, -math.Sqrt(3)/2)

	vA := complex(phase[0].Real, phase[0].Imag)
	vB := complex(phase[1].Real, phase[1].Imag)
	vC := complex(phase[2].Real, phase[2].Imag)

	v1 := (vA + a*vB + a2*vC) / complex(3.0, 0)

	realPart := real(v1)
	imagPart := imag(v1)

	if math.IsNaN(realPart) || math.IsInf(realPart, 0) {
		realPart = 0
	}
	if math.IsNaN(imagPart) || math.IsInf(imagPart, 0) {
		imagPart = 0
	}

	mag := math.Sqrt(realPart*realPart + imagPart*imagPart)
	if math.IsNaN(mag) || math.IsInf(mag, 0) {
		mag = 0
	}

	angle := math.Atan2(imagPart, realPart)
	if math.IsNaN(angle) || math.IsInf(angle, 0) {
		angle = 0
	}

	return models.ComplexPhasor{
		Real:      realPart,
		Imag:      imagPart,
		Magnitude: mag,
		Angle:     angle,
	}
}

func GetTotalPanicPrevented() uint64 {
	return atomic.LoadUint64(&totalPanicPrevented)
}
