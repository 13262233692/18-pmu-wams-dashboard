package prony

import (
	"errors"
	"math"
	"math/cmplx"
	"sync"

	"wams-dashboard/internal/models"
)

type PronyAnalyzer struct {
	Order       int
	WindowSize  int
	Fs          float64
	MinFreqHz   float64
	MaxFreqHz   float64
	MinDamping  float64
	MaxDamping  float64

	mu sync.Mutex
}

func NewPronyAnalyzer(order, windowSize int, fs float64) *PronyAnalyzer {
	if order <= 0 {
		order = 12
	}
	if windowSize <= order*4 {
		windowSize = order * 4
	}
	if fs <= 0 {
		fs = 50.0
	}

	return &PronyAnalyzer{
		Order:      order,
		WindowSize: windowSize,
		Fs:         fs,
		MinFreqHz:  0.2,
		MaxFreqHz:  2.5,
		MinDamping: -5.0,
		MaxDamping:  2.0,
	}
}

type PronyResult struct {
	Modes       []models.PronyMode
	Residual    float64
	RSS         float64
	SignalPower float64
	Success     bool
	Message     string
}

func (pa *PronyAnalyzer) Analyze(signal []float64) (*PronyResult, error) {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	N := len(signal)
	if N < pa.Order*2 {
		return nil, errors.New("signal too short for Prony analysis")
	}
	if N > pa.WindowSize {
		signal = signal[N-pa.WindowSize:]
		N = pa.WindowSize
	}

	mean := 0.0
	for _, x := range signal {
		mean += x
	}
	mean /= float64(N)
	centered := make([]float64, N)
	signalPower := 0.0
	for i, x := range signal {
		centered[i] = x - mean
		signalPower += centered[i] * centered[i]
	}
	signalPower /= float64(N)

	if signalPower < 1e-12 {
		return &PronyResult{
			Modes:       []models.PronyMode{},
			Residual:    0,
			RSS:         0,
			SignalPower: signalPower,
			Success:     true,
			Message:     "signal is DC only, no oscillations detected",
		}, nil
	}

	p := pa.Order
	L := N - p

	Y := make([]complex128, L)
	for i := 0; i < L; i++ {
		Y[i] = complex(centered[i+p], 0)
	}

	H := make([][]complex128, L)
	for i := 0; i < L; i++ {
		H[i] = make([]complex128, p)
		for j := 0; j < p; j++ {
			H[i][j] = complex(centered[i+p-1-j], 0)
		}
	}

	a, err := solveLinearSystem(H, Y)
	if err != nil {
		return nil, err
	}

	coeffs := make([]complex128, p+1)
	coeffs[0] = complex(1, 0)
	for i := 0; i < p; i++ {
		coeffs[i+1] = -a[i]
	}

	roots := findPolynomialRoots(coeffs)

	dt := 1.0 / pa.Fs
	modes := make([]models.PronyMode, 0, p)

	for _, z := range roots {
		if cmplx.Abs(z) < 1e-10 {
			continue
		}

		logZ := cmplx.Log(z)
		s := logZ / complex(dt, 0)
		sigma := real(s)
		omega := imag(s)
		freq := omega / (2 * math.Pi)

		if freq < 0 {
			freq = -freq
		}

		if freq < pa.MinFreqHz || freq > pa.MaxFreqHz {
			continue
		}

		absFreq := math.Abs(freq)
		dampingFactor := -sigma
		var dampingRatio float64
		if absFreq > 1e-6 {
			dampingRatio = dampingFactor / (2 * math.Pi * absFreq)
		} else {
			continue
		}

		if dampingRatio < pa.MinDamping || dampingRatio > pa.MaxDamping {
			continue
		}

		modes = append(modes, models.PronyMode{
			Frequency:     freq,
			DampingRatio:  dampingRatio,
			DampingFactor: dampingFactor,
			Amplitude:     math.NaN(),
			Phase:         math.NaN(),
		})
	}

	if len(modes) == 0 {
		return &PronyResult{
			Modes:       []models.PronyMode{},
			Residual:    1.0,
			RSS:         signalPower * float64(N),
			SignalPower: signalPower,
			Success:     true,
			Message:     "no modes in frequency band",
		}, nil
	}

	B := make([][]complex128, N)
	for i := 0; i < N; i++ {
		B[i] = make([]complex128, len(modes))
		for j, m := range modes {
			sigma_j := -m.DampingFactor
			omega_j := 2 * math.Pi * m.Frequency
			s_j := complex(sigma_j, omega_j)
			t := float64(i) * dt
			B[i][j] = cmplx.Exp(s_j * complex(t, 0))
		}
	}

	yVec := make([]complex128, N)
	for i := 0; i < N; i++ {
		yVec[i] = complex(centered[i], 0)
	}

	amplitudes, err := solveLinearSystem(B, yVec)
	if err == nil && len(amplitudes) == len(modes) {
		totalEnergy := 0.0
		for i := range modes {
			amp := amplitudes[i]
			modes[i].Amplitude = 2 * cmplx.Abs(amp)
			modes[i].Phase = cmplx.Phase(amp) * 180 / math.Pi
			totalEnergy += modes[i].Amplitude * modes[i].Amplitude
		}
		if totalEnergy > 1e-12 {
			for i := range modes {
				modes[i].EnergyRatio = (modes[i].Amplitude * modes[i].Amplitude) / totalEnergy
			}
		}
	}

	rss := 0.0
	for i := 0; i < N; i++ {
		recon := 0.0
		for _, m := range modes {
			if math.IsNaN(m.Amplitude) {
				continue
			}
			sigma_j := -m.DampingFactor
			omega_j := 2 * math.Pi * m.Frequency
			t := float64(i) * dt
			phaseRad := m.Phase * math.Pi / 180
			recon += m.Amplitude * math.Exp(sigma_j*t) * math.Cos(omega_j*t+phaseRad)
		}
		diff := centered[i] - recon
		rss += diff * diff
	}
	rss /= float64(N)

	residual := 1.0
	if signalPower > 1e-12 {
		residual = math.Sqrt(rss / signalPower)
	}

	sortModesByEnergy(modes)

	return &PronyResult{
		Modes:       modes,
		Residual:    residual,
		RSS:         rss,
		SignalPower: signalPower,
		Success:     true,
		Message:     "ok",
	}, nil
}

func solveLinearSystem(A [][]complex128, b []complex128) ([]complex128, error) {
	M := len(A)
	if M == 0 {
		return nil, errors.New("empty matrix")
	}
	N := len(A[0])
	if len(b) != M {
		return nil, errors.New("dimension mismatch")
	}

	if M >= N {
		return solveLeastSquares(A, b, M, N)
	}

	return solveMinNorm(A, b, M, N)
}

func solveLeastSquares(A [][]complex128, b []complex128, M, N int) ([]complex128, error) {
	AtA := make([][]complex128, N)
	for i := 0; i < N; i++ {
		AtA[i] = make([]complex128, N)
		for j := 0; j < N; j++ {
			sum := complex(0, 0)
			for k := 0; k < M; k++ {
				sum += cmplx.Conj(A[k][i]) * A[k][j]
			}
			AtA[i][j] = sum
		}
	}

	Atb := make([]complex128, N)
	for i := 0; i < N; i++ {
		sum := complex(0, 0)
		for k := 0; k < M; k++ {
			sum += cmplx.Conj(A[k][i]) * b[k]
		}
		Atb[i] = sum
	}

	return solveComplexCholesky(AtA, Atb, N)
}

func solveMinNorm(A [][]complex128, b []complex128, M, N int) ([]complex128, error) {
	AAt := make([][]complex128, M)
	for i := 0; i < M; i++ {
		AAt[i] = make([]complex128, M)
		for j := 0; j < M; j++ {
			sum := complex(0, 0)
			for k := 0; k < N; k++ {
				sum += A[i][k] * cmplx.Conj(A[j][k])
			}
			AAt[i][j] = sum
		}
	}

	y, err := solveComplexCholesky(AAt, b, M)
	if err != nil {
		return nil, err
	}

	x := make([]complex128, N)
	for i := 0; i < N; i++ {
		sum := complex(0, 0)
		for k := 0; k < M; k++ {
			sum += cmplx.Conj(A[k][i]) * y[k]
		}
		x[i] = sum
	}
	return x, nil
}

func solveComplexCholesky(A [][]complex128, b []complex128, N int) ([]complex128, error) {
	L := make([][]complex128, N)
	for i := 0; i < N; i++ {
		L[i] = make([]complex128, N)
	}

	for i := 0; i < N; i++ {
		for j := 0; j <= i; j++ {
			sum := A[i][j]
			for k := 0; k < j; k++ {
				sum -= L[i][k] * cmplx.Conj(L[j][k])
			}
			if i == j {
				re := real(sum)
				if re <= 0 {
					return solveComplexGauss(A, b, N)
				}
				L[i][j] = complex(math.Sqrt(re), 0)
			} else {
				L[i][j] = sum / L[j][j]
			}
		}
	}

	y := make([]complex128, N)
	for i := 0; i < N; i++ {
		sum := b[i]
		for k := 0; k < i; k++ {
			sum -= L[i][k] * y[k]
		}
		y[i] = sum / L[i][i]
	}

	x := make([]complex128, N)
	for i := N - 1; i >= 0; i-- {
		sum := y[i]
		for k := i + 1; k < N; k++ {
			sum -= cmplx.Conj(L[k][i]) * x[k]
		}
		x[i] = sum / L[i][i]
	}
	return x, nil
}

func solveComplexGauss(A [][]complex128, b []complex128, N int) ([]complex128, error) {
	Aug := make([][]complex128, N)
	for i := 0; i < N; i++ {
		Aug[i] = make([]complex128, N+1)
		copy(Aug[i][:N], A[i])
		Aug[i][N] = b[i]
	}

	for col := 0; col < N; col++ {
		maxRow := col
		maxVal := cmplx.Abs(Aug[col][col])
		for row := col + 1; row < N; row++ {
			val := cmplx.Abs(Aug[row][col])
			if val > maxVal {
				maxVal = val
				maxRow = row
			}
		}
		if maxVal < 1e-15 {
			continue
		}
		if maxRow != col {
			Aug[col], Aug[maxRow] = Aug[maxRow], Aug[col]
		}

		pivot := Aug[col][col]
		for j := col; j <= N; j++ {
			Aug[col][j] /= pivot
		}

		for row := 0; row < N; row++ {
			if row == col {
				continue
			}
			factor := Aug[row][col]
			if cmplx.Abs(factor) < 1e-15 {
				continue
			}
			for j := col; j <= N; j++ {
				Aug[row][j] -= factor * Aug[col][j]
			}
		}
	}

	x := make([]complex128, N)
	for i := 0; i < N; i++ {
		x[i] = Aug[i][N]
	}
	return x, nil
}

func findPolynomialRoots(coeffs []complex128) []complex128 {
	n := len(coeffs) - 1
	if n < 1 {
		return []complex128{}
	}
	if n == 1 {
		return []complex128{-coeffs[1] / coeffs[0]}
	}

	roots := make([]complex128, n)
	for i := 0; i < n; i++ {
		roots[i] = complex(0.5*math.Cos(2*math.Pi*float64(i+1)/float64(n+1)),
			0.5*math.Sin(2*math.Pi*float64(i+1)/float64(n+1)))
	}

	for iter := 0; iter < 200; iter++ {
		converged := true
		for i := 0; i < n; i++ {
			r := roots[i]

			p := coeffs[0]
			dp := complex(0, 0)
			for k := 1; k <= n; k++ {
				dp = dp*r + p
				p = p*r + coeffs[k]
			}

			denom := complex(1, 0)
			for j := 0; j < n; j++ {
				if j != i {
					denom *= (r - roots[j])
				}
			}

			dr := complex(0, 0)
			if cmplx.Abs(dp) > 1e-15 && cmplx.Abs(denom) > 1e-15 {
				dr = p / (dp * denom)
			}

			if cmplx.Abs(dr) > 1e-14 {
				converged = false
			}

			roots[i] = r - dr*0.7

			if math.IsNaN(real(roots[i])) || math.IsInf(real(roots[i]), 0) ||
				math.IsNaN(imag(roots[i])) || math.IsInf(imag(roots[i]), 0) {
				roots[i] = complex(0.1*math.Cos(2*math.Pi*float64(i+1)/float64(n+1)),
					0.1*math.Sin(2*math.Pi*float64(i+1)/float64(n+1)))
			}
		}
		if converged {
			break
		}
	}

	return roots
}

func sortModesByEnergy(modes []models.PronyMode) {
	for i := 0; i < len(modes); i++ {
		maxIdx := i
		for j := i + 1; j < len(modes); j++ {
			if modes[j].EnergyRatio > modes[maxIdx].EnergyRatio {
				maxIdx = j
			}
		}
		modes[i], modes[maxIdx] = modes[maxIdx], modes[i]
	}
}

func ComputeDampingGradient(history []float64, window int) float64 {
	n := len(history)
	if n < window {
		window = n
	}
	if window < 3 {
		return 0
	}

	values := history[n-window:]
	m := float64(window)

	sumX := 0.0
	sumY := 0.0
	sumXY := 0.0
	sumXX := 0.0
	for i, y := range values {
		x := float64(i)
		sumX += x
		sumY += y
		sumXY += x * y
		sumXX += x * x
	}

	denom := m*sumXX - sumX*sumX
	if math.Abs(denom) < 1e-15 {
		return 0
	}

	return (m*sumXY - sumX*sumY) / denom
}

func ComputeOscillationAmplitude(signal []float64) float64 {
	n := len(signal)
	if n < 4 {
		return 0
	}

	mean := 0.0
	for _, x := range signal {
		mean += x
	}
	mean /= float64(n)

	maxVal := signal[0] - mean
	minVal := signal[0] - mean
	for _, x := range signal {
		d := x - mean
		if d > maxVal {
			maxVal = d
		}
		if d < minVal {
			minVal = d
		}
	}

	return (maxVal - minVal) / 2.0
}
