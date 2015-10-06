package beat

import (
	"time"

	cfg "github.com/elastic/filebeat/config"
	"github.com/elastic/filebeat/input"
	"github.com/elastic/libbeat/logp"
)

type Spooler struct {
	Filebeat      *Filebeat
	running       bool
	nextFlushTime time.Time
	spool         []*input.FileEvent
	SpoolChan     chan *input.FileEvent
}

func NewSpooler(filebeat *Filebeat) *Spooler {
	spooler := &Spooler{
		Filebeat: filebeat,
		running:  false,
	}

	config := &spooler.Filebeat.FbConfig.Filebeat

	// Set the next flush time
	spooler.nextFlushTime = time.Now().Add(config.IdleTimeoutDuration)
	spooler.SpoolChan = make(chan *input.FileEvent, 16)

	return spooler
}

func (spooler *Spooler) Config() error {
	config := &spooler.Filebeat.FbConfig.Filebeat

	// Set default pool size if value not set
	if config.SpoolSize == 0 {
		config.SpoolSize = cfg.DefaultSpoolSize
	}

	// Set default idle timeout if not set
	if config.IdleTimeout == "" {
		logp.Debug("spooler", "Set idleTimeoutDuration to %s", cfg.DefaultIdleTimeout)
		// Set it to default
		config.IdleTimeoutDuration = cfg.DefaultIdleTimeout
	} else {
		var err error

		config.IdleTimeoutDuration, err = time.ParseDuration(config.IdleTimeout)

		if err != nil {
			logp.Warn("Failed to parse idle timeout duration '%s'. Error was: %v", config.IdleTimeout, err)
			return err
		}
	}

	return nil
}

// Run runs the spooler
// It heartbeats periodically. If the last flush was longer than
// 'IdleTimeoutDuration' time ago, then we'll force a flush to prevent us from
// holding on to spooled events for too long.
func (s *Spooler) Run() {

	config := &s.Filebeat.FbConfig.Filebeat

	// Enable running
	s.running = true

	// Sets up ticket channel
	ticker := time.NewTicker(config.IdleTimeoutDuration / 2)

	s.spool = make([]*input.FileEvent, 0, config.SpoolSize)

	// Loops until running is set to false
	for {
		if !s.running {
			break
		}

		select {
		case event := <-s.SpoolChan:
			s.spool = append(s.spool, event)

			// Spooler is full -> flush
			if len(s.spool) == cap(s.spool) {
				s.flush()
			}
		case <-ticker.C:
			// Flush periodically
			if now := time.Now(); now.After(s.nextFlushTime) {
				s.flush()
			}
		}
	}

	// Flush again before exiting spooler and closes channel
	s.flush()
	close(s.SpoolChan)
}

// Stop stops the spooler. Flushes events before stopping
func (s *Spooler) Stop() {
	s.running = false
}

// flush flushes all event and sends them to the publisher
func (s *Spooler) flush() {
	// Checks if any new objects
	if len(s.spool) > 0 {

		// copy buffer
		tmpCopy := make([]*input.FileEvent, len(s.spool))
		copy(tmpCopy, s.spool)

		// clear buffer
		s.spool = s.spool[:0]

		// send
		s.Filebeat.publisherChan <- tmpCopy
	}
	s.nextFlushTime = time.Now().Add(s.Filebeat.FbConfig.Filebeat.IdleTimeoutDuration)
}
