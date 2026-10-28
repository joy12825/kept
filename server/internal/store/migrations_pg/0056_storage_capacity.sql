-- v1.7.10: the most a storage added on the page may hold, in bytes; 0 is
-- no limit. Storages of the deployment take KEPT_STORAGE_<NAME>_CAPACITY.
ALTER TABLE storages ADD COLUMN capacity BIGINT NOT NULL DEFAULT 0;
