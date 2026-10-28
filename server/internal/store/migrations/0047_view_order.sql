-- v1.6.5: saved views can be dragged into an order. 0 for every view means
-- none was set (pinyin order, as before); once ordered, new views go last.
ALTER TABLE saved_views ADD COLUMN position INTEGER NOT NULL DEFAULT 0;
