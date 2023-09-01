package dlna

import (
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

type Repository struct {
	SceneFinder     SceneFinder
	FileGetter      models.FileGetter
	StudioFinder    StudioFinder
	TagFinder       TagFinder
	PerformerFinder PerformerFinder
	MovieFinder     MovieFinder
}

type Status struct {
	Running bool `json:"running"`
	// If not currently running, time until it will be started. If running, time until it will be stopped
	Until              *time.Time `json:"until"`
	RecentIPAddresses  []string   `json:"recentIPAddresses"`
	AllowedIPAddresses []*Dlnaip  `json:"allowedIPAddresses"`
}

type Dlnaip struct {
	IPAddress string `json:"ipAddress"`
	// Time until IP will be no longer allowed/disallowed
	Until *time.Time `json:"until"`
}

type dmsConfig struct {
	Path                string
	IfNames             []string
	Http                string
	FriendlyName        string
	LogHeaders          bool
	StallEventSubscribe bool
	NotifyInterval      time.Duration
	VideoSortOrder      string
}

type sceneServer interface {
	StreamSceneDirect(scene *models.Scene, w http.ResponseWriter, r *http.Request)
	ServeScreenshot(scene *models.Scene, w http.ResponseWriter, r *http.Request)
}

type Config interface {
	GetDLNAInterfaces() []string
	GetDLNAServerName() string
	GetDLNADefaultIPWhitelist() []string
	GetVideoSortOrder() string
}

type Service struct {
	txnManager     txn.Manager
	repository     Repository
	config         Config
	sceneServer    sceneServer
	ipWhitelistMgr *ipWhitelistManager

	server  *Server
	running bool
	mutex   sync.Mutex

	startTimer *time.Timer
	startTime  *time.Time
	stopTimer  *time.Timer
	stopTime   *time.Time
}

func (s *Service) getInterfaces() ([]net.Interface, error) {
	var ifs []net.Interface
	var err error
	ifNames := s.config.GetDLNAInterfaces()

	if len(ifNames) == 0 {
		ifs, err = net.Interfaces()
	} else {
		for _, n := range ifNames {
			if_, err := net.InterfaceByName(n)
			if err != nil {
				return nil, fmt.Errorf("error getting interface for name %s: %s", n, err.Error())
			}

			if if_ != nil {
				ifs = append(ifs, *if_)
			}
		}
	}

	if err != nil {
		return nil, err
	}

	var tmp []net.Interface
	for _, if_ := range ifs {
		if if_.Flags&net.FlagUp == 0 || if_.MTU <= 0 {
			continue
		}
		tmp = append(tmp, if_)
	}
	ifs = tmp
	return ifs, nil
}

func (s *Service) init() error {
	friendlyName := s.config.GetDLNAServerName()
	if friendlyName == "" {
		friendlyName = "stash"
	}

	var dmsConfig = &dmsConfig{
		Path:           "",
		IfNames:        s.config.GetDLNADefaultIPWhitelist(),
		Http:           ":1338",
		FriendlyName:   friendlyName,
		LogHeaders:     false,
		NotifyInterval: 30 * time.Second,
		VideoSortOrder: s.config.GetVideoSortOrder(),
	}

	interfaces, err := s.getInterfaces()
	if err != nil {
		return err
	}

	s.server = &Server{
		txnManager:         s.txnManager,
		sceneServer:        s.sceneServer,
		repository:         s.repository,
		ipWhitelistManager: s.ipWhitelistMgr,
		Interfaces:         interfaces,
		HTTPConn: func() net.Listener {
			conn, err := net.Listen("tcp", dmsConfig.Http)
			if err != nil {
				logger.Error(err.Error())
			}
			return conn
		}(),
		FriendlyName:   dmsConfig.FriendlyName,
		RootObjectPath: filepath.Clean(dmsConfig.Path),
		LogHeaders:     dmsConfig.LogHeaders,
		// Icons: []Icon{
		// 	{
		// 		Width:    48,
		// 		Height:   48,
		// 		Depth:    8,
		// 		Mimetype: "image/png",
		// 		//ReadSeeker: readIcon(config.Config.Interfaces.DLNA.ServiceImage, 48),
		// 	},
		// 	{
		// 		Width:    128,
		// 		Height:   128,
		// 		Depth:    8,
		// 		Mimetype: "image/png",
		// 		//ReadSeeker: readIcon(config.Config.Interfaces.DLNA.ServiceImage, 128),
		// 	},
		// },
		StallEventSubscribe: dmsConfig.StallEventSubscribe,
		NotifyInterval:      dmsConfig.NotifyInterval,
		VideoSortOrder:      dmsConfig.VideoSortOrder,
	}

	return nil
}

// func getIconReader(fn string) (io.Reader, error) {
// 	b, err := assets.ReadFile("dlna/" + fn + ".png")
// 	return bytes.NewReader(b), err
// }

// func readIcon(path string, size uint) *bytes.Reader {
// 	r, err := getIconReader(path)
// 	if err != nil {
// 		panic(err)
// 	}
// 	imageData, _, err := image.Decode(r)
// 	if err != nil {
// 		panic(err)
// 	}
// 	return resizeImage(imageData, size)
// }

// func resizeImage(imageData image.Image, size uint) *bytes.Reader {
// 	img := resize.Resize(size, size, imageData, resize.Lanczos3)
// 	var buff bytes.Buffer
// 	png.Encode(&buff, img)
// 	return bytes.NewReader(buff.Bytes())
// }

// NewService initialises and returns a new DLNA service.
func NewService(txnManager txn.Manager, repo Repository, cfg Config, sceneServer sceneServer) *Service {
	ret := &Service{
		txnManager:  txnManager,
		repository:  repo,
		sceneServer: sceneServer,
		config:      cfg,
		ipWhitelistMgr: &ipWhitelistManager{
			config: cfg,
		},
		mutex: sync.Mutex{},
	}

	return ret
}

// Start starts the DLNA service. If duration is provided, then the service
// is stopped after the duration has elapsed.
func (s *Service) Start(duration *time.Duration) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if !s.running {
		if err := s.init(); err != nil {
			logger.Error(err)
			return err
		}

		go func() {
			logger.Info("Starting DLNA")
			if err := s.server.Serve(); err != nil {
				logger.Error(err)
			}
		}()
		s.running = true

		if s.startTimer != nil {
			s.startTimer.Stop()
			s.startTimer = nil
			s.startTime = nil
		}
	}

	if duration != nil {
		// clear the existing stop timer
		if s.stopTimer != nil {
			s.stopTimer.Stop()
			s.stopTime = nil
		}

		if s.stopTimer == nil {
			s.stopTimer = time.AfterFunc(*duration, func() {
				s.Stop(nil)
			})
			t := time.Now().Add(*duration)
			s.stopTime = &t
		}
	}

	return nil
}

// Stop stops the DLNA service. If duration is provided, then the service
// is started after the duration has elapsed.
func (s *Service) Stop(duration *time.Duration) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.running {
		logger.Info("Stopping DLNA")
		err := s.server.Close()
		if err != nil {
			logger.Error(err)
		}
		s.running = false

		if s.stopTimer != nil {
			s.stopTimer.Stop()
			s.stopTimer = nil
			s.stopTime = nil
		}
	}

	if duration != nil {
		// clear the existing stop timer
		if s.startTimer != nil {
			s.startTimer.Stop()
		}

		if s.startTimer == nil {
			s.startTimer = time.AfterFunc(*duration, func() {
				if err := s.Start(nil); err != nil {
					logger.Warnf("error restarting DLNA server: %v", err)
				}
			})
			t := time.Now().Add(*duration)
			s.startTime = &t
		}
	}
}

// IsRunning returns true if the DLNA service is running.
func (s *Service) IsRunning() bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.running
}

func (s *Service) Status() *Status {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	ret := &Status{
		Running:            s.running,
		RecentIPAddresses:  s.ipWhitelistMgr.getRecent(),
		AllowedIPAddresses: s.ipWhitelistMgr.getTempAllowed(),
	}

	if s.startTime != nil {
		t := *s.startTime
		ret.Until = &t
	}

	if s.stopTime != nil {
		t := *s.stopTime
		ret.Until = &t
	}

	return ret
}

func (s *Service) AddTempDLNAIP(pattern string, duration *time.Duration) {
	s.ipWhitelistMgr.allowPattern(pattern, duration)
}

func (s *Service) RemoveTempDLNAIP(pattern string) bool {
	return s.ipWhitelistMgr.removePattern(pattern)
}
