package botruntime

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/karma-234/mtg-bot/internal/service"
	"gopkg.in/telebot.v4"
)

type TaskManager struct {
	tasks map[int64]context.CancelFunc
	mu    sync.RWMutex
}

func NewTaskManager() *TaskManager {
	return &TaskManager{tasks: make(map[int64]context.CancelFunc)}
}

func (m *TaskManager) Schedule(b *telebot.Bot, duration time.Duration, chat *telebot.Chat, srv *service.MerchantService) {
	m.mu.Lock()
	if cancel, exists := m.tasks[chat.ID]; exists {
		cancel()
		log.Printf("Existing task for chat %d cancelled", chat.ID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	m.tasks[chat.ID] = cancel
	m.mu.Unlock()

	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer func() {
			ticker.Stop()
			m.mu.Lock()
			delete(m.tasks, chat.ID)
			m.mu.Unlock()
			log.Printf("Task for chat %d completed or cancelled", chat.ID)
		}()

		messageCount := 1
		for {
			select {
			case t := <-ticker.C:
				log.Printf("Executing scheduled task for chat %s", chat.Username)
				resp, err := srv.GetLatestOrders(nil)
				if err != nil {
					log.Printf("Failed to get Orders to : %v", err)
					if _, sendErr := b.Send(chat, "Failed to fetch orders\n"+"TimeStamp"+t.Format("15:04:05")+"\n"+"Message count:"+fmt.Sprint(messageCount)); sendErr != nil {
						log.Printf("Error sending fetch failure message to chat %d: %v", chat.ID, sendErr)
					}
					continue
				}
				if !resp.OK() {
					log.Printf("Error from merchant: %v", resp.Error())
				}
				msg := service.FormatOrdersMessage(resp)
				if _, sendErr := b.Send(chat, "Here is the latest MTG news...\n"+"TimeStamp"+t.Format("15:04:05")+"\n"+"Message count:"+fmt.Sprint(messageCount)+"\n\n"+msg); sendErr != nil {
					log.Printf("Error sending periodic update to chat %d: %v", chat.ID, sendErr)
				}
			case <-ctx.Done():
				log.Printf("Task for chat %v Completed", chat.Username)
				if _, err := b.Send(chat, "Task for user "+chat.Username+" completed"); err != nil {
					log.Printf("Error sending completion message to chat %d: %v", chat.ID, err)
				}
				return
			}
			messageCount++
		}
	}()
}

func (m *TaskManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for chatID, cancel := range m.tasks {
		cancel()
		log.Printf("Cancelled task for chat %d", chatID)
	}
}
