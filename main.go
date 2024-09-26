package main

import (
	"log"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Player struct {
	ID           int       `json:"id"`
	X            float64   `json:"x"`
	Y            float64   `json:"y"`
	LastPushTime time.Time // Время последнего действия "push"
	LastPullTime time.Time // Время последнего действия "pull"
}

type CapturePoint struct {
	X               float64   `json:"x"`
	Y               float64   `json:"y"`
	Radius          float64   `json:"radius"`
	IsCaptured      bool      `json:"isCaptured"`
	CapturingPlayer int       `json:"capturingPlayer"`
	CaptureStart    time.Time `json:"captureStart"`
	EnterTime       time.Time `json:"enterTime"`
}

type GameState struct {
	Players       []Player       `json:"players"`
	CapturePoints []CapturePoint `json:"capturePoints"`
	Points1       int            `json:"points1"`
	Points2       int            `json:"points2"`
}

var (
	clients       = make(map[*websocket.Conn]bool)
	players       = make(map[int]*Player)
	capturePoints = []CapturePoint{
		{X: 300, Y: 200, Radius: 50},
		{X: 800, Y: 600, Radius: 50},
	}
	points1  = 0
	points2  = 0
	mutex    = &sync.Mutex{}
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	lastUpdate = time.Now()
)

func main() {
	http.HandleFunc("/ws", handleConnections)
	go gameLoop()
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Ошибка при апгрейде соединения:", err)
		return
	}
	defer conn.Close()

	// Генерация нового playerID
	playerID := len(players) + 1

	// Отправка playerID клиенту
	err = conn.WriteJSON(map[string]interface{}{
		"playerID": playerID,
	})
	if err != nil {
		log.Println("Ошибка при отправке playerID:", err)
		return
	}

	// Добавляем нового игрока
	clients[conn] = true
	players[playerID] = &Player{
		ID: playerID,
		X:  400,
		Y:  400,
	}

	// Чтение и обработка сообщений от клиента
	for {
		var msg map[string]interface{}
		err := conn.ReadJSON(&msg)
		if err != nil {
			log.Println("Ошибка при чтении сообщения:", err)
			delete(clients, conn)
			delete(players, playerID)
			break
		}

		// Обработка сообщений, связанных с действиями игрока
		if id, ok := msg["id"].(float64); ok {
			player := players[int(id)]

			if x, ok := msg["x"].(float64); ok {
				player.X = x
			}
			if y, ok := msg["y"].(float64); ok {
				player.Y = y
			}
			if action, ok := msg["action"].(string); ok {
				handleAction(player, action)
			}
		}
	}
}

func handleAction(player *Player, action string) {
	currentTime := time.Now()
	cooldown := 2 * time.Second

	switch action {
	case "push":
		if currentTime.Sub(player.LastPushTime) > cooldown {
			player.LastPushTime = currentTime
			log.Printf("Игрок %d использовал push", player.ID)
			applyPush(player)
			broadcastGameState() // Обновляем состояние игры после push
		}
	case "pull":
		if currentTime.Sub(player.LastPullTime) > cooldown {
			player.LastPullTime = currentTime
			log.Printf("Игрок %d использовал pull", player.ID)
			applyPull(player)
			broadcastGameState() // Обновляем состояние игры после pull
		}
	}
}

// Функция для отправки обновленного состояния всем клиентам
func broadcastGameState() {
	mutex.Lock()
	defer mutex.Unlock()

	gameState := GameState{
		Players:       getPlayersState(),
		CapturePoints: capturePoints,
		Points1:       points1,
		Points2:       points2,
	}

	for client := range clients {
		err := client.WriteJSON(gameState)
		if err != nil {
			log.Println("Ошибка при отправке состояния игры:", err)
			client.Close()
			delete(clients, client)
		}
	}
}

func applyPush(player *Player) {
	// Ищем ближайшего игрока
	var closestPlayer *Player
	closestDistance := math.MaxFloat64

	for _, p := range players {
		if p.ID != player.ID {
			distance := math.Sqrt(math.Pow(player.X-p.X, 2) + math.Pow(player.Y-p.Y, 2))
			if distance < closestDistance {
				closestDistance = distance
				closestPlayer = p
			}
		}
	}

	if closestPlayer != nil && closestDistance < 100 { // Проверка дистанции
		// Рассчитываем вектор отталкивания
		dx := closestPlayer.X - player.X
		dy := closestPlayer.Y - player.Y
		length := math.Sqrt(dx*dx + dy*dy)
		if length != 0 {
			dx /= length
			dy /= length
		}

		// Определяем силу отталкивания
		pushStrength := 1000.0
		distance := closestDistance // Используем найденную дистанцию

		// Применяем отталкивание с плавным перемещением
		go func() {
			steps := 10                    // Количество шагов для плавного перемещения
			delay := 16 * time.Millisecond // Задержка между шагами

			for i := 0; i < steps; i++ {
				mutex.Lock()

				// Логгируем текущее состояние
				log.Printf("Applying push to target: %d (step %d)\n", closestPlayer.ID, i+1)

				// Обновляем позицию
				closestPlayer.X += (dx / distance) * pushStrength / float64(steps)
				closestPlayer.Y += (dy / distance) * pushStrength / float64(steps)

				mutex.Unlock()
				time.Sleep(delay)
			}
		}()

		log.Printf("Игрок %d оттолкнул игрока %d", player.ID, closestPlayer.ID)
	}
}

func applyPull(player *Player) {
	// Ищем ближайшего игрока
	var closestPlayer *Player
	closestDistance := math.MaxFloat64

	for _, p := range players {
		if p.ID != player.ID {
			distance := math.Sqrt(math.Pow(player.X-p.X, 2) + math.Pow(player.Y-p.Y, 2))
			if distance < closestDistance {
				closestDistance = distance
				closestPlayer = p
			}
		}
	}

	if closestPlayer != nil && closestDistance < 100 { // Проверка дистанции
		// Рассчитываем вектор притяжения
		dx := player.X - closestPlayer.X
		dy := player.Y - closestPlayer.Y
		length := math.Sqrt(dx*dx + dy*dy)
		if length != 0 {
			dx /= length
			dy /= length
		}

		// Определяем силу притяжения
		pullStrength := 1000.0
		distance := closestDistance

		// Применяем плавное притяжение
		go func() {
			steps := 10                    // Количество шагов для плавного перемещения
			delay := 16 * time.Millisecond // Задержка между шагами

			for i := 0; i < steps; i++ {
				mutex.Lock()

				// Логгируем текущее состояние
				log.Printf("Applying pull to target: %d (step %d)\n", closestPlayer.ID, i+1)

				// Обновляем позицию
				closestPlayer.X += (dx / distance) * pullStrength / float64(steps)
				closestPlayer.Y += (dy / distance) * pullStrength / float64(steps)

				mutex.Unlock()
				time.Sleep(delay)
			}
		}()

		log.Printf("Игрок %d притянул игрока %d", player.ID, closestPlayer.ID)
	}
}

func gameLoop() {
	go checkCapturePoints()
	for {
		time.Sleep(16 * time.Millisecond)
		mutex.Lock()

		gameState := GameState{
			Players:       getPlayersState(),
			CapturePoints: capturePoints,
			Points1:       points1,
			Points2:       points2,
		}

		for client := range clients {
			err := client.WriteJSON(gameState)
			if err != nil {
				log.Println("Ошибка при отправке состояния игры:", err)
				client.Close()
				delete(clients, client)
			}
		}

		mutex.Unlock()
	}
}

func getPlayersState() []Player {
	var playerList []Player
	for _, player := range players {
		playerList = append(playerList, *player)
	}
	return playerList
}

func checkCapturePoints() {
	for {
		time.Sleep(100 * time.Millisecond)
		mutex.Lock() // Блокируем доступ к данным
		for i := range capturePoints {
			cp := &capturePoints[i]

			player1InZone := isPlayerInZone(players[1], cp)
			player2InZone := isPlayerInZone(players[2], cp)

			if player1InZone && player2InZone {
				cp.EnterTime = time.Time{} // Сброс таймера, если оба игрока в зоне
			} else if player1InZone {
				if cp.EnterTime.IsZero() {
					cp.EnterTime = time.Now()
				}
				if time.Since(cp.EnterTime) >= 5*time.Second {
					cp.IsCaptured = true
					cp.CapturingPlayer = 1
					cp.CaptureStart = time.Now()
					cp.EnterTime = time.Time{} // Сброс таймера захвата
				}
			} else if player2InZone {
				if cp.EnterTime.IsZero() {
					cp.EnterTime = time.Now()
				}
				if time.Since(cp.EnterTime) >= 5*time.Second {
					cp.IsCaptured = true
					cp.CapturingPlayer = 2
					cp.CaptureStart = time.Now()
					cp.EnterTime = time.Time{} // Сброс таймера захвата
				}
			} else {
				cp.EnterTime = time.Time{} // Сброс таймера, если игрок вышел из зоны
			}

			// Начисление очков за захваченные точки
			if cp.IsCaptured {
				if cp.CapturingPlayer == 1 {
					if time.Since(cp.CaptureStart) >= 5*time.Second {
						points1++
						cp.CaptureStart = time.Now() // Обновляем время последнего начисления очков
					}
				} else if cp.CapturingPlayer == 2 {
					if time.Since(cp.CaptureStart) >= 5*time.Second {
						points2++
						cp.CaptureStart = time.Now() // Обновляем время последнего начисления очков
					}
				}
			}

		}
		mutex.Unlock()
	}
}
func isPlayerInZone(player *Player, cp *CapturePoint) bool {
	if player == nil {
		return false
	}
	distance := math.Sqrt(math.Pow(player.X-cp.X, 2) + math.Pow(player.Y-cp.Y, 2))
	return distance <= cp.Radius
}
