package main

import (
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

type TrendResponse struct {
	Character string            `json:"character"`
	Info      []*TrendCondition `json:"info"`
	Warning   []*TrendCondition `json:"warning"`
	Critical  []*TrendCondition `json:"critical"`
}

type TrendCondition struct {
	ID        int   `json:"isu_id"`
	Timestamp int64 `json:"timestamp"`
}

var trendCache = struct {
	trend []TrendResponse
	sync.RWMutex
}{
	trend: []TrendResponse{},
}

// GET /api/trend
// ISUの性格毎の最新のコンディション情報
func getTrend(c echo.Context) error {
	trendCache.RLock()
	defer trendCache.RUnlock()

	return c.JSON(http.StatusOK, trendCache.trend)
}

func resetTrendCacheTicker() {
	t := time.NewTicker(time.Millisecond * trendTickerTime)

	for {
		<-t.C

		characterList := []Isu{}
		err := db.Select(&characterList, "SELECT `character` FROM `isu` GROUP BY `character`")
		if err != nil {
		}

		res := []TrendResponse{}

		for _, character := range characterList {
			isuList := []Isu{}
			err = db.Select(&isuList,
				"SELECT `id`, `jia_isu_uuid` FROM `isu` WHERE `character` = ?",
				character.Character,
			)
			if err != nil {
			}

			// type IsuAndCondition struct {
			// 	ID                 int       `db:"id"`
			// 	Timestamp          time.Time `db:"timestamp"`
			// 	ConditionLevelList string    `db:"condition_level_list"`
			// }
			// isuAndConditionList := []IsuAndCondition{}
			// err = db.Select(&isuAndConditionList,
			// 	"SELECT isu.id AS id,"+
			// 		" GROUP_CONCAT(isu_condition.condition_level ORDER BY isu_condition.timestamp DESC) AS condition_level_list,"+
			// 		" MAX(isu_condition.timestamp) AS timestamp"+
			// 		" FROM isu"+
			// 		" INNER JOIN isu_condition ON isu.jia_isu_uuid = isu_condition.jia_isu_uuid"+
			// 		" WHERE isu.character = ? GROUP BY isu.jia_isu_uuid",
			// 	character.Character,
			// )

			characterInfoIsuConditions := []*TrendCondition{}
			characterWarningIsuConditions := []*TrendCondition{}
			characterCriticalIsuConditions := []*TrendCondition{}

			// for _, isuAndCondition := range isuAndConditionList {
			// 	trendCondition := TrendCondition{
			// 		ID:        isuAndCondition.ID,
			// 		Timestamp: isuAndCondition.Timestamp.Unix(),
			// 	}
			// 	conditionLevel := strings.Split(isuAndCondition.ConditionLevelList, ",")[0]
			// 	switch conditionLevel {
			// 	case "info":
			// 		characterInfoIsuConditions = append(characterInfoIsuConditions, &trendCondition)
			// 	case "warning":
			// 		characterWarningIsuConditions = append(characterWarningIsuConditions, &trendCondition)
			// 	case "critical":
			// 		characterCriticalIsuConditions = append(characterCriticalIsuConditions, &trendCondition)
			// 	}
			// }

			for _, isu := range isuList {
				conditions := []IsuCondition{}
				err = db.Select(&conditions,
					"SELECT `timestamp`, `condition_level` FROM `isu_condition` WHERE `jia_isu_uuid` = ? ORDER BY timestamp DESC LIMIT 1",
					isu.JIAIsuUUID,
				)
				if err != nil {
				}

				if len(conditions) > 0 {
					isuLastCondition := conditions[0]
					// conditionLevel, err := calculateConditionLevel(isuLastCondition.Condition)
					// if err != nil {
					// }
					trendCondition := TrendCondition{
						ID:        isu.ID,
						Timestamp: isuLastCondition.Timestamp.Unix(),
					}
					switch isuLastCondition.ConditionLevel {
					case "info":
						characterInfoIsuConditions = append(characterInfoIsuConditions, &trendCondition)
					case "warning":
						characterWarningIsuConditions = append(characterWarningIsuConditions, &trendCondition)
					case "critical":
						characterCriticalIsuConditions = append(characterCriticalIsuConditions, &trendCondition)
					}
				}

			}

			sort.Slice(characterInfoIsuConditions, func(i, j int) bool {
				return characterInfoIsuConditions[i].Timestamp > characterInfoIsuConditions[j].Timestamp
			})
			sort.Slice(characterWarningIsuConditions, func(i, j int) bool {
				return characterWarningIsuConditions[i].Timestamp > characterWarningIsuConditions[j].Timestamp
			})
			sort.Slice(characterCriticalIsuConditions, func(i, j int) bool {
				return characterCriticalIsuConditions[i].Timestamp > characterCriticalIsuConditions[j].Timestamp
			})
			res = append(res,
				TrendResponse{
					Character: character.Character,
					Info:      characterInfoIsuConditions,
					Warning:   characterWarningIsuConditions,
					Critical:  characterCriticalIsuConditions,
				})
		}

		trendCache.Lock()
		trendCache.trend = res
		trendCache.Unlock()
	}
}
