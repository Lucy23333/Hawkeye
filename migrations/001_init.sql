CREATE TABLE IF NOT EXISTS events (
  id INT AUTO_INCREMENT PRIMARY KEY,
  filename VARCHAR(255) NOT NULL,
  capture_time DATETIME NOT NULL,
  ai_analysis TEXT,
  device_id VARCHAR(50) DEFAULT 'CAM-01',
  status VARCHAR(20) DEFAULT 'open',
  tags VARCHAR(255) DEFAULT '',
  analysis_status VARCHAR(20) DEFAULT 'pending',
  analysis_error TEXT,
  analysis_attempts INT DEFAULT 0,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_events_capture_time (capture_time),
  INDEX idx_events_device_status (device_id, status),
  UNIQUE KEY uniq_events_filename (filename)
);

CREATE TABLE IF NOT EXISTS devices (
  id VARCHAR(50) PRIMARY KEY,
  last_seen DATETIME,
  enabled TINYINT(1) DEFAULT 1,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
