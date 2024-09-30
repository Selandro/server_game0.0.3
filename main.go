package main

import (
	"encoding/json"
	"log"
	"math"
	"net"
	"sync"
	"time"
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
	conn          *net.UDPConn // Глобальная переменная для UDP соединения
	players       = make(map[int]*Player)
	clientAddrs   = make(map[int]*net.UDPAddr) // Хранение адресов клиентов
	capturePoints = []CapturePoint{
		{X: 300, Y: 200, Radius: 50},
		{X: 800, Y: 600, Radius: 50},
	}
	points1 = 0
	points2 = 0
	mutex   = &sync.Mutex{}
	udpAddr = net.UDPAddr{
		Port: 8080,
		IP:   net.ParseIP("0.0.0.0"),
	}
)

func main() {
	var err error
	conn, err = net.ListenUDP("udp", &udpAddr)
	if err != nil {
		log.Fatal("Ошибка при прослушивании UDP:", err)
	}
	defer conn.Close()

	go gameLoop()
	go checkCapturePoints()

	buffer := make([]byte, 2048)
	for {
		n, addr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			log.Println("Ошибка при чтении UDP:", err)
			continue
		}

		var msg map[string]interface{}
		if err := json.Unmarshal(buffer[:n], &msg); err != nil {
			log.Println("Ошибка при разборе JSON:", err)
			continue
		}

		handleUDPMessage(addr, msg)
	}
}

func handleUDPMessage(addr *net.UDPAddr, msg map[string]interface{}) {
	playerID := 0
	if id, ok := msg["id"].(float64); ok {
		playerID = int(id)
	} else {
		// Если id не указан, присваиваем новый ID
		mutex.Lock()
		playerID = len(players) + 1
		players[playerID] = &Player{
			ID: playerID,
			X:  400,
			Y:  400,
		}
		clientAddrs[playerID] = addr // Сохраняем адрес клиента
		log.Printf("Игрок %d подключился", playerID)
		mutex.Unlock()

		// Отправляем присвоенный playerID обратно клиенту
		response := map[string]interface{}{
			"id": playerID,
		}
		sendUDPMessage(addr, response)
		return
	}

	player := players[playerID]

	// Обработка сообщений, связанных с действиями игрока
	if x, ok := msg["x"].(float64); ok {
		player.X = x
	}
	if y, ok := msg["y"].(float64); ok {
		player.Y = y
	}
	if action, ok := msg["action"].(string); ok {
		handleAction(player, action)
	}

	// Отправка состояния игры обратно игроку
	sendGameState(addr)
}

func sendUDPMessage(addr *net.UDPAddr, msg map[string]interface{}) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Println("Ошибка сериализации сообщения:", err)
		return
	}
	_, err = conn.WriteToUDP(data, addr)
	if err != nil {
		log.Println("Ошибка отправки сообщения клиенту:", err)
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
		}
	case "pull":
		if currentTime.Sub(player.LastPullTime) > cooldown {
			player.LastPullTime = currentTime
			log.Printf("Игрок %d использовал pull", player.ID)
			applyPull(player)
		}
	}
}
func sendGameState(addr *net.UDPAddr) {
	mutex.Lock()
	defer mutex.Unlock()

	gameState := GameState{
		Players:       getPlayersState(),
		CapturePoints: capturePoints,
		Points1:       points1,
		Points2:       points2,
	}

	data, err := json.Marshal(gameState)
	if err != nil {
		log.Println("Ошибка при сериализации состояния игры:", err)
		return
	}

	// Проверка, что адрес клиента существует в клиентских адресах
	if addr == nil {
		log.Println("Ошибка: адрес клиента nil")
		return
	}

	_, err = conn.WriteToUDP(data, addr)
	if err != nil {
		log.Println("Ошибка при отправке состояния игры:", err)
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
	for {
		time.Sleep(16 * time.Millisecond)
		mutex.Lock()

		gameState := GameState{
			Players:       getPlayersState(),
			CapturePoints: capturePoints,
			Points1:       points1,
			Points2:       points2,
		}

		// Отправка состояния игры всем игрокам
		for id, player := range players {
			data, err := json.Marshal(gameState)
			if err != nil {
				log.Println("Ошибка при сериализации состояния игры:", err)
				continue
			}

			// Пример использования переменной player
			log.Printf("Отправка состояния игры игроку %d, координаты: (%.2f, %.2f)", player.ID, player.X, player.Y)

			// Отправляем состояние игры игроку по его адресу
			if addr, ok := clientAddrs[id]; ok {
				_, err = conn.WriteToUDP(data, addr)
				if err != nil {
					log.Println("Ошибка при отправке состояния игроку:", err)
				}
			}
		}

		mutex.Unlock()
	}
}

func getPlayersState() []Player {
	var playersState []Player
	for _, player := range players {
		playersState = append(playersState, *player)
	}
	return playersState
}

func checkCapturePoints() {
	for {
		mutex.Lock()

		// Логика захвата точек
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
