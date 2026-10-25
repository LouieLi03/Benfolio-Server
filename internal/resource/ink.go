package resource

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"math"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
	"github.com/LouieLi03/Benfolio-Server/internal/jsonvalue"
	"github.com/LouieLi03/Benfolio-Server/internal/position"
	"github.com/LouieLi03/Benfolio-Server/internal/syncvalue"
)

// DecodeInk validates the portable schema without changing the source bytes.
func DecodeInk(ctx context.Context, data []byte) (domain.Ink, error) {
	var ink domain.Ink
	if len(data) == 0 || int64(len(data)) > domain.MaxInkBytes {
		return ink, domain.ErrInvalidResource
	}
	canonical, err := jsonvalue.Normalize(data)
	if err != nil {
		return ink, domain.ErrInvalidResource
	}
	object, err := jsonvalue.Object(canonical, "schemaVersion", "documentVersionID", "sourceSHA256", "pageIndex", "coordinateSpace", "strokes")
	if err != nil || json.Unmarshal(canonical, &ink) != nil || ink.SchemaVersion != 1 ||
		!syncvalue.ValidID(ink.DocumentVersionID) || !syncvalue.ValidSHA256(ink.SourceSHA256) ||
		ink.PageIndex == nil || *ink.PageIndex < 0 || ink.CoordinateSpace != domain.InkCoordinateSpace ||
		len(ink.Strokes) < 1 || len(ink.Strokes) > 1000 {
		return domain.Ink{}, domain.ErrInvalidResource
	}
	var strokes []json.RawMessage
	if json.Unmarshal(object["strokes"], &strokes) != nil {
		return domain.Ink{}, domain.ErrInvalidResource
	}
	ids := make(map[string]bool, len(strokes))
	totalPoints := 0
	for i, stroke := range ink.Strokes {
		if err := ctx.Err(); err != nil {
			return domain.Ink{}, err
		}
		fields, err := jsonvalue.Object(strokes[i], "id", "tool", "color", "width", "points")
		if err != nil || !syncvalue.ValidID(stroke.ID) || ids[stroke.ID] ||
			(stroke.Tool != "pen" && stroke.Tool != "highlighter") || !rgba(stroke.Color) ||
			!finite(stroke.Width) || stroke.Width <= 0 || stroke.Width > 0.05 || len(stroke.Points) < 1 {
			return domain.Ink{}, domain.ErrInvalidResource
		}
		ids[stroke.ID] = true
		totalPoints += len(stroke.Points)
		if totalPoints > 20000 {
			return domain.Ink{}, domain.ErrInvalidResource
		}
		var points []json.RawMessage
		if json.Unmarshal(fields["points"], &points) != nil {
			return domain.Ink{}, domain.ErrInvalidResource
		}
		lastTime := float64(-1)
		for j, point := range stroke.Points {
			if j%128 == 0 {
				if err := ctx.Err(); err != nil {
					return domain.Ink{}, err
				}
			}
			fields, err := jsonvalue.Object(points[j], "x", "y", "pressure", "timeMS")
			if err != nil || fields["x"] == nil || fields["y"] == nil ||
				!finite(point.X, point.Y) || point.X < 0 || point.X > 1 || point.Y < 0 || point.Y > 1 ||
				!position.ValidFraction(point.Pressure) {
				return domain.Ink{}, domain.ErrInvalidResource
			}
			for _, raw := range fields {
				if string(raw) == "null" {
					return domain.Ink{}, domain.ErrInvalidResource
				}
			}
			if point.TimeMS != nil {
				if !finite(*point.TimeMS) || *point.TimeMS < 0 || *point.TimeMS > 86400000 || *point.TimeMS < lastTime {
					return domain.Ink{}, domain.ErrInvalidResource
				}
				lastTime = *point.TimeMS
			}
		}
	}
	return ink, nil
}

func rgba(value string) bool {
	if len(value) != 9 || value[0] != '#' {
		return false
	}
	_, err := hex.DecodeString(value[1:])
	return err == nil
}

func finite(values ...float64) bool {
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return true
}
