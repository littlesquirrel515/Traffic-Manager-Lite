ALTER TABLE traffic_samples ADD COLUMN batch_id TEXT NOT NULL DEFAULT '';
CREATE TABLE collection_batches(instance_id INTEGER PRIMARY KEY REFERENCES instances(id), batch_id TEXT NOT NULL);
CREATE INDEX samples_batch ON traffic_samples(instance_id,batch_id);
