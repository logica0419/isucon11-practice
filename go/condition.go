package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
)

type GetIsuConditionResponse struct {
	JIAIsuUUID     string `json:"jia_isu_uuid"`
	IsuName        string `json:"isu_name"`
	Timestamp      int64  `json:"timestamp"`
	IsSitting      bool   `json:"is_sitting"`
	Condition      string `json:"condition"`
	ConditionLevel string `json:"condition_level"`
	Message        string `json:"message"`
}

// ISUのコンディションをDBから取得
func getIsuConditionsFromDB(db *sqlx.DB, jiaIsuUUID string, endTime time.Time, conditionLevel map[string]interface{}, startTime time.Time,
	limit int, isuName string) ([]*GetIsuConditionResponse, error) {

	conditions := []IsuCondition{}
	var err error

	condLevelKeys := []string{}
	for key := range conditionLevel {
		condLevelKeys = append(condLevelKeys, key)
	}

	if startTime.IsZero() {
		sql, params, err := sqlx.In(
			"SELECT * FROM `isu_condition` WHERE `jia_isu_uuid` = ?"+
				"	AND `timestamp` < ?"+
				"	AND `condition_level` IN (?)"+
				"	ORDER BY `timestamp` DESC"+
				" LIMIT ?",
			jiaIsuUUID, endTime, condLevelKeys, limit)
		if err != nil {
			return nil, fmt.Errorf("db error: %v", err)
		}

		err = db.Select(&conditions, sql, params...)
	} else {
		sql, params, err := sqlx.In(
			"SELECT * FROM `isu_condition` WHERE `jia_isu_uuid` = ?"+
				"	AND `timestamp` < ?"+
				"	AND ? <= `timestamp`"+
				"	AND `condition_level` IN (?)"+
				"	ORDER BY `timestamp` DESC"+
				" LIMIT ?",
			jiaIsuUUID, endTime, startTime, condLevelKeys, limit)
		if err != nil {
			return nil, fmt.Errorf("db error: %v", err)
		}

		err = db.Select(&conditions, sql, params...)
	}
	if err != nil {
		return nil, fmt.Errorf("db error: %v", err)
	}

	conditionsResponse := []*GetIsuConditionResponse{}
	for _, c := range conditions {
		// cLevel, err := calculateConditionLevel(c.Condition)
		// if err != nil {
		// 	continue
		// }

		data := GetIsuConditionResponse{
			JIAIsuUUID:     c.JIAIsuUUID,
			IsuName:        isuName,
			Timestamp:      c.Timestamp.Unix(),
			IsSitting:      c.IsSitting,
			Condition:      c.Condition,
			ConditionLevel: c.ConditionLevel,
			Message:        c.Message,
		}
		conditionsResponse = append(conditionsResponse, &data)

	}

	// if len(conditionsResponse) > limit {
	// 	conditionsResponse = conditionsResponse[:limit]
	// }

	return conditionsResponse, nil
}

// ISUのコンディションの文字列からコンディションレベルを計算
func calculateConditionLevel(condition string) (string, error) {
	var conditionLevel string

	warnCount := strings.Count(condition, "=true")
	switch warnCount {
	case 0:
		conditionLevel = conditionLevelInfo
	case 1, 2:
		conditionLevel = conditionLevelWarning
	case 3:
		conditionLevel = conditionLevelCritical
	default:
		return "", fmt.Errorf("unexpected warn count")
	}

	return conditionLevel, nil
}

// GET /api/condition/:jia_isu_uuid
// ISUのコンディションを取得
func getIsuConditions(c echo.Context) error {
	jiaUserID, errStatusCode, err := getUserIDFromSession(c)
	if err != nil {
		if errStatusCode == http.StatusUnauthorized {
			return c.String(http.StatusUnauthorized, "you are not signed in")
		}

		// c.Logger().Error(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	jiaIsuUUID := c.Param("jia_isu_uuid")
	if jiaIsuUUID == "" {
		return c.String(http.StatusBadRequest, "missing: jia_isu_uuid")
	}

	endTimeInt64, err := strconv.ParseInt(c.QueryParam("end_time"), 10, 64)
	if err != nil {
		return c.String(http.StatusBadRequest, "bad format: end_time")
	}
	endTime := time.Unix(endTimeInt64, 0)
	conditionLevelCSV := c.QueryParam("condition_level")
	if conditionLevelCSV == "" {
		return c.String(http.StatusBadRequest, "missing: condition_level")
	}
	conditionLevel := map[string]interface{}{}
	for _, level := range strings.Split(conditionLevelCSV, ",") {
		conditionLevel[level] = struct{}{}
	}

	startTimeStr := c.QueryParam("start_time")
	var startTime time.Time
	if startTimeStr != "" {
		startTimeInt64, err := strconv.ParseInt(startTimeStr, 10, 64)
		if err != nil {
			return c.String(http.StatusBadRequest, "bad format: start_time")
		}
		startTime = time.Unix(startTimeInt64, 0)
	}

	var isuName string
	err = db.Get(&isuName,
		"SELECT name FROM `isu` WHERE `jia_isu_uuid` = ? AND `jia_user_id` = ?",
		jiaIsuUUID, jiaUserID,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c.String(http.StatusNotFound, "not found: isu")
		}

		//c.Logger().Errorf("db error: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	conditionsResponse, err := getIsuConditionsFromDB(db, jiaIsuUUID, endTime, conditionLevel, startTime, conditionLimit, isuName)
	if err != nil {
		// c.Logger().Errorf("db error: %v", err)
		return c.NoContent(http.StatusInternalServerError)
	}
	return c.JSON(http.StatusOK, conditionsResponse)
}

// ISUのコンディションの文字列がcsv形式になっているか検証
func isValidConditionFormat(conditionStr string) bool {

	keys := []string{"is_dirty=", "is_overweight=", "is_broken="}
	const valueTrue = "true"
	const valueFalse = "false"

	idxCondStr := 0

	for idxKeys, key := range keys {
		if !strings.HasPrefix(conditionStr[idxCondStr:], key) {
			return false
		}
		idxCondStr += len(key)

		if strings.HasPrefix(conditionStr[idxCondStr:], valueTrue) {
			idxCondStr += len(valueTrue)
		} else if strings.HasPrefix(conditionStr[idxCondStr:], valueFalse) {
			idxCondStr += len(valueFalse)
		} else {
			return false
		}

		if idxKeys < (len(keys) - 1) {
			if conditionStr[idxCondStr] != ',' {
				return false
			}
			idxCondStr++
		}
	}

	return (idxCondStr == len(conditionStr))
}

type PostIsuConditionRequest struct {
	IsSitting bool   `json:"is_sitting"`
	Condition string `json:"condition"`
	Message   string `json:"message"`
	Timestamp int64  `json:"timestamp"`
}

var (
	isuIDValidMap = struct {
		validMap map[string]int
		sync.Mutex
	}{
		validMap: map[string]int{},
	}
)

// POST /api/condition/:jia_isu_uuid
// ISUからのコンディションを受け取る
func postIsuCondition(c echo.Context) error {
	c.NoContent(http.StatusAccepted)

	jiaIsuUUID := c.Param("jia_isu_uuid")
	if jiaIsuUUID == "" {
		return c.String(http.StatusBadRequest, "missing: jia_isu_uuid")
	}

	req := []PostIsuConditionRequest{}
	err := c.Bind(&req)
	if err != nil {
		return c.String(http.StatusBadRequest, "bad request body")
	} else if len(req) == 0 {
		return c.String(http.StatusBadRequest, "bad request body")
	}

	// tx, err := db.Beginx()
	// if err != nil {
	// 	c.Logger().Errorf("db error: %v", err)
	// 	return c.NoContent(http.StatusInternalServerError)
	// }
	// defer tx.Rollback()

	isuIDValidMap.Lock()
	if _, ok := isuIDValidMap.validMap[jiaIsuUUID]; !ok {
		var id int
		err = db.Get(&id, "SELECT `id` FROM `isu` WHERE `jia_isu_uuid` = ? LIMIT 1", jiaIsuUUID)
		if err != nil {
			isuIDValidMap.Unlock()
			// c.Logger().Errorf("db error: %v", err)
			return c.NoContent(http.StatusInternalServerError)
		}
		isuIDValidMap.validMap[jiaIsuUUID] = 1
	}
	isuIDValidMap.Unlock()

	for _, cond := range req {
		timestamp := time.Unix(cond.Timestamp, 0)

		if !isValidConditionFormat(cond.Condition) {
			return c.String(http.StatusBadRequest, "bad request body")
		}

		condLevel, err := calculateConditionLevel(cond.Condition)
		if err != nil {
			continue
		}

		isuCondition := IsuCondition{
			JIAIsuUUID:     jiaIsuUUID,
			Timestamp:      timestamp,
			IsSitting:      cond.IsSitting,
			Condition:      cond.Condition,
			Message:        cond.Message,
			ConditionLevel: condLevel,
		}

		insertDataStore.Lock()
		insertDataStore.data = append(insertDataStore.data, isuCondition)
		insertDataStore.Unlock()

		// _, err = tx.Exec(
		// 	"INSERT INTO `isu_condition`"+
		// 		"	(`jia_isu_uuid`, `timestamp`, `is_sitting`, `condition`, `message`)"+
		// 		"	VALUES (?, ?, ?, ?, ?)",
		// 	jiaIsuUUID, timestamp, cond.IsSitting, cond.Condition, cond.Message)
		// if err != nil {
		// 	c.Logger().Errorf("db error: %v", err)
		// 	return c.NoContent(http.StatusInternalServerError)
		// }

	}

	// err = tx.Commit()
	// if err != nil {
	// 	c.Logger().Errorf("db error: %v", err)
	// 	return c.NoContent(http.StatusInternalServerError)
	// }

	return nil
}

type insertData struct {
	data []IsuCondition
	sync.Mutex
}

var insertDataStore = insertData{
	data: []IsuCondition{},
}

func insertConditionTicker() {
	t := time.NewTicker(insertTickerTime * time.Millisecond) //1秒周期の ticker
	defer t.Stop()

	for {
		<-t.C

		if len(insertDataStore.data) == 0 {
			continue
		}

		go func() {
			tx, err := db.Beginx()
			if err != nil {
				log.Printf("db error: %v", err)
				return
			}
			defer tx.Rollback()

			insertDataStore.Lock()
			defer insertDataStore.Unlock()

			_, err = tx.NamedExec(
				"INSERT INTO `isu_condition`"+
					"	(`jia_isu_uuid`, `timestamp`, `is_sitting`, `condition`, `message`, `condition_level`)"+
					"	VALUES (:jia_isu_uuid, :timestamp, :is_sitting, :condition, :message, :condition_level)",
				insertDataStore.data)

			err = tx.Commit()
			if err != nil {
				log.Printf("db error: %v", err)
				return
			}

			insertDataStore.data = []IsuCondition{}
			return
		}()
	}
}
