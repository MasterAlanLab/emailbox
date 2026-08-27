// Package job 提供批量任务的提交、执行、停止与进度订阅。
//
// 任务状态全部入库（jobs / job_items / job_events），进程内存里只放
// 「正在跑的那些任务的取消函数」和「订阅者的唤醒通道」。这样做的直接好处是：
// 浏览器断线、用户刷新页面、服务重启，进度都不会丢——这正是 outlookEmail
// 把进度放内存所付出的代价（它因此被迫单 worker 部署，刷新页面即丢失进度）。
package job

import "sync"

// Broker 只负责「某个任务有新事件了」这一个信号，不传事件内容。
//
// 为什么不直接把事件推给订阅者：推送要处理慢订阅者，而处理慢订阅者的唯一
// 正确做法是丢事件——一旦丢了，SSE 流就断了档。改成「只发信号、内容从
// job_events 读」之后，实时推送与断线重连变成同一条代码路径：
// 都是「从 last_seq 之后读」。慢订阅者最多是少收到几次唤醒，读的时候一次补齐。
type Broker struct {
	mu   sync.Mutex
	subs map[string]map[chan struct{}]struct{}
}

func NewBroker() *Broker {
	return &Broker{subs: make(map[string]map[chan struct{}]struct{})}
}

// Subscribe 订阅某个任务的变更信号，返回通道与退订函数。
func (b *Broker) Subscribe(jobID string) (<-chan struct{}, func()) {
	// 容量 1 就够：信号是「有变化」而不是「变化了几次」，
	// 攒下来的多次通知合并成一次读取反而更省。
	ch := make(chan struct{}, 1)

	b.mu.Lock()
	if b.subs[jobID] == nil {
		b.subs[jobID] = make(map[chan struct{}]struct{})
	}
	b.subs[jobID][ch] = struct{}{}
	b.mu.Unlock()

	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if set, ok := b.subs[jobID]; ok {
			delete(set, ch)
			if len(set) == 0 {
				delete(b.subs, jobID)
			}
		}
	}
}

// Notify 唤醒该任务的所有订阅者。非阻塞：通道满了说明对方还没消费上一次信号，
// 而信号本身没有信息量，丢掉不影响正确性。
func (b *Broker) Notify(jobID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs[jobID] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
