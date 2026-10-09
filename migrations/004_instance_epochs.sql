CREATE TABLE instance_epochs(instance_id INTEGER PRIMARY KEY REFERENCES instances(id), epoch_id TEXT NOT NULL, boot_estimate TEXT NOT NULL, observed_at TEXT NOT NULL);
