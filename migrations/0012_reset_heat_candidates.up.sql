-- 清空 heat_candidates 表:旧代码(无词频过滤)写入了大量泛化词(数据/模型/合作等),
-- 新代码已加入 gse 词频自动过滤(freq>2000排除),需要从零累计。
TRUNCATE heat_candidates;
