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
	FlipX        bool      `json:"flipX"`
	LastPushTime time.Time // Время последнего действия "push"
	LastPullTime time.Time // Время последнего действия "pull"
	Name         string    `json:"name"`   // Добавляем JSON-тег для имени
	Skin         string    `json:"skin"`   // Добавляем JSON-тег для скина
	Points       int       `json:"points"` // Добавляем поле для очков
}

type CapturePoint struct {
	X                      float64   `json:"x"`
	Y                      float64   `json:"y"`
	Radius                 float64   `json:"radius"`
	IsCaptured             bool      `json:"isCaptured"`
	CapturingPlayer        int       `json:"capturingPlayer"`
	CurrentCapturingPlayer int       `json:"currentCapturingPlayer"` // Добавлен JSON-тег
	CaptureStart           time.Time `json:"captureStart"`
	EnterTime              time.Time `json:"enterTime"`
}

type GameState struct {
	Players       []Player       `json:"players"`
	CapturePoints []CapturePoint `json:"capturePoints"`
}

var (
	conn          *net.UDPConn // Глобальная переменная для UDP соединения
	players       = make(map[int]*Player)
	clientAddrs   = make(map[int]*net.UDPAddr) // Хранение адресов клиентов
	capturePoints = []CapturePoint{
		{X: 300, Y: 200, Radius: 50},
		{X: 800, Y: 600, Radius: 50},
		{X: 550, Y: 400, Radius: 50},
	}

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
			ID:   playerID,
			X:    400,
			Y:    400,
			Name: msg["name"].(string),
			Skin: msg["skin"].(string),
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
	if flipX, ok := msg["flipX"].(bool); ok {
		player.FlipX = flipX
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
		time.Sleep(10 * time.Millisecond)
		mutex.Lock()

		gameState := GameState{
			Players:       getPlayersState(),
			CapturePoints: capturePoints,
		}

		// Отправка состояния игры всем игрокам
		for id, player := range players {
			data, err := json.Marshal(gameState)
			if err != nil {
				log.Println("Ошибка при сериализации состояния игры:", err)
				continue
			}

			// Пример использования переменной player
			log.Printf("Отправка состояния игры игроку %d, координаты: (%.2f, %.2f,%s)", player.ID, player.X, player.Y, player.FlipX)

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

			// Считаем, кто находится в зоне захвата
			var capturingPlayer *Player
			for _, player := range players {
				if isPlayerInZone(player, cp) {
					if capturingPlayer == nil {
						capturingPlayer = player
					} else {
						// Если больше одного игрока в зоне, сбрасываем захват
						capturingPlayer = nil
						cp.EnterTime = time.Time{} // Сброс таймера
						break
					}
				}
			}

			// Если только один игрок в зоне, продолжаем захват
			if capturingPlayer != nil {
				cp.CurrentCapturingPlayer = capturingPlayer.ID
				if cp.EnterTime.IsZero() {
					cp.EnterTime = time.Now()
				}
				if time.Since(cp.EnterTime) >= 5*time.Second {
					if !cp.IsCaptured || cp.CapturingPlayer != capturingPlayer.ID {
						cp.IsCaptured = true
						cp.CapturingPlayer = capturingPlayer.ID
						cp.CaptureStart = time.Now()
						cp.EnterTime = time.Time{} // Сброс таймера захвата
					}
				}
			} else {
				// Никто не захватывает, сбрасываем таймер
				cp.EnterTime = time.Time{}
				cp.CurrentCapturingPlayer = 0
			}

			// Начисление очков за захваченные точки
			if cp.IsCaptured {
				// Проверяем, сколько времени точка удерживается и начисляем очки
				if time.Since(cp.CaptureStart) >= 5*time.Second {
					if cp.CapturingPlayer != 0 {
						player := players[cp.CapturingPlayer]

						// Начисляем очки захватчику
						player.Points++ // Начисляем очки игроку

						// Обновляем время последнего начисления очков
						cp.CaptureStart = time.Now()
					}
				}
			}
		}

		mutex.Unlock()
		time.Sleep(100 * time.Millisecond) // Задержка между проверками
	}
}
func isPlayerInZone(player *Player, cp *CapturePoint) bool {
	if player == nil {
		return false
	}
	distance := math.Sqrt(math.Pow(player.X-cp.X, 2) + math.Pow(player.Y-cp.Y, 2))
	return distance <= cp.Radius
}
