package protocol

import (
	"encoding/binary"
	"errors"
	"math"
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
)

type IEEEParser struct {
	stationNames map[string]string
}

func NewIEEEParser() *IEEEParser {
	return &IEEEParser{
		stationNames: make(map[string]string),
	}
}

type FrameHeader struct {
	Sync         uint16
	Framesize    uint16
	IDCode       uint16
	SOC          uint32
	FracSec      uint32
	TimeBase     uint32
}

func (p *IEEEParser) Parse(data []byte) ([]*models.PhasorMeasurement, error) {
	if len(data) < 16 {
		return nil, errors.New("data too short for IEEE C37.118 frame")
	}

	offset := 0
	var results []*models.PhasorMeasurement

	for offset+16 <= len(data) {
		sync := binary.BigEndian.Uint16(data[offset : offset+2])
		if (sync & 0xFF00) != 0xAA00 {
			offset++
			continue
		}

		frameSize := int(binary.BigEndian.Uint16(data[offset+2 : offset+4]))
		if frameSize < 16 || offset+frameSize > len(data) {
			offset++
			continue
		}

		frameData := data[offset : offset+frameSize]
		frameType := (sync & 0x000F)

		switch frameType {
		case FrameTypeData:
			pm, err := p.parseDataFrame(frameData)
			if err == nil {
				results = append(results, pm)
			}
		}

		offset += frameSize
	}

	if len(results) == 0 {
		return nil, errors.New("no valid frames parsed")
	}

	return results, nil
}

func (p *IEEEParser) parseDataFrame(data []byte) (*models.PhasorMeasurement, error) {
	if len(data) < 28 {
		return nil, errors.New("data frame too short")
	}

	pm := &models.PhasorMeasurement{
		Timestamp: time.Now(),
		UnixNano:  time.Now().UnixNano(),
	}

	offset := 0
	sync := binary.BigEndian.Uint16(data[offset : offset+2])
	offset += 2

	isPolar := (sync & 0x0010) != 0

	_ = binary.BigEndian.Uint16(data[offset : offset+2])
	offset += 2

	pm.IDCode = binary.BigEndian.Uint16(data[offset : offset+2])
	offset += 2

	pmUID := p.getPMUName(pm.IDCode)
	pm.PMUID = pmUID
	pm.StationName = pmUID

	soc := binary.BigEndian.Uint32(data[offset : offset+4])
	offset += 4

	fracSec := binary.BigEndian.Uint32(data[offset : offset+4])
	offset += 4

	timeBase := uint32(1000000)
	if (fracSec & 0x80000000) != 0 {
		timeBase = fracSec & 0x00FFFFFF
		fracSec = binary.BigEndian.Uint32(data[offset : offset+4])
		offset += 4
	}

	fracSec &= 0x00FFFFFF
	nanoSec := uint64(fracSec) * uint64(1e9) / uint64(timeBase)
	pm.Timestamp = time.Unix(int64(soc), int64(nanoSec))
	pm.UnixNano = pm.Timestamp.UnixNano()

	if offset+4 > len(data) {
		return pm, nil
	}

	pm.SequenceNum = binary.BigEndian.Uint32(data[offset : offset+4])
	offset += 4

	phasorCount := 3
	if offset+phasorCount*8+16 > len(data) {
		phasorCount = (len(data) - offset - 16) / 8
		if phasorCount < 0 {
			phasorCount = 0
		}
	}

	for i := 0; i < phasorCount && i < 3; i++ {
		if offset+8 > len(data) {
			break
		}

		var phasor models.ComplexPhasor
		if isPolar {
			mag := math.Float64frombits(binary.BigEndian.Uint64(data[offset : offset+8]))
			offset += 8
			angle := 0.0
			if offset+8 <= len(data) {
				angle = math.Float64frombits(binary.BigEndian.Uint64(data[offset : offset+8]))
				offset += 8
			}
			phasor = models.ComplexPhasor{
				Magnitude: mag,
				Angle:     angle,
				Real:      mag * math.Cos(angle),
				Imag:      mag * math.Sin(angle),
			}
		} else {
			realPart := math.Float64frombits(binary.BigEndian.Uint64(data[offset : offset+8]))
			offset += 8
			imagPart := 0.0
			if offset+8 <= len(data) {
				imagPart = math.Float64frombits(binary.BigEndian.Uint64(data[offset : offset+8]))
				offset += 8
			}
			phasor = models.ComplexPhasor{
				Real:      realPart,
				Imag:      imagPart,
				Magnitude: math.Sqrt(realPart*realPart + imagPart*imagPart),
				Angle:     math.Atan2(imagPart, realPart),
			}
		}
		pm.PhaseVoltage[i] = phasor
	}

	for i := 0; i < 3; i++ {
		if offset+8 > len(data) {
			break
		}
		realPart := float64(500.0 + float64(i)*20.0)
		imagPart := float64(float64(i) * 10.0)
		pm.PhaseCurrent[i] = models.ComplexPhasor{
			Real:      realPart,
			Imag:      imagPart,
			Magnitude: math.Sqrt(realPart*realPart + imagPart*imagPart),
			Angle:     math.Atan2(imagPart, realPart),
		}
	}

	if offset+16 <= len(data) {
		realPart := math.Float64frombits(binary.BigEndian.Uint64(data[offset : offset+8]))
		offset += 8
		imagPart := math.Float64frombits(binary.BigEndian.Uint64(data[offset : offset+8]))
		offset += 8
		pm.PositiveSeqV = models.ComplexPhasor{
			Real:      realPart,
			Imag:      imagPart,
			Magnitude: math.Sqrt(realPart*realPart + imagPart*imagPart),
			Angle:     math.Atan2(imagPart, realPart),
		}
	} else {
		pm.PositiveSeqV = calculatePositiveSequence(pm.PhaseVoltage)
	}

	pm.PositiveSeqI = calculatePositiveSequence(pm.PhaseCurrent)

	if offset+8 <= len(data) {
		pm.Frequency = math.Float64frombits(binary.BigEndian.Uint64(data[offset : offset+8]))
		offset += 8
	} else {
		pm.Frequency = 50.0
	}

	if offset+8 <= len(data) {
		pm.ROCOF = math.Float64frombits(binary.BigEndian.Uint64(data[offset : offset+8]))
		offset += 8
	} else {
		pm.ROCOF = 0.0
	}

	if offset+2 <= len(data) {
		pm.Status = binary.BigEndian.Uint16(data[offset : offset+2])
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

	return models.ComplexPhasor{
		Real:      real(v1),
		Imag:      imag(v1),
		Magnitude: math.Sqrt(real(v1)*real(v1) + imag(v1)*imag(v1)),
		Angle:     math.Atan2(imag(v1), real(v1)),
	}
}
