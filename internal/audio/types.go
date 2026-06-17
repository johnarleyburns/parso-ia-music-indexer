package audio

type QualityScore struct {
	Composite   float64
	SNR         float64
	Centroid    float64
	CrestFactor float64
}

const (
	QualityHighFidelity      = 0.7
	QualityHistoricalVintage = 0.3
	QualityUnusable          = 0.3
)

func (q QualityScore) Tier() string {
	if q.Composite > QualityHighFidelity {
		return "high_fidelity"
	}
	if q.Composite >= QualityHistoricalVintage {
		return "historical_vintage"
	}
	return "unusable"
}

func (q QualityScore) IsUsable() bool {
	return q.Composite >= QualityHistoricalVintage
}
