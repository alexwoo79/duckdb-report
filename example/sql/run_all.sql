-- 手工一键复跑；参数默认值位于 01_join.sql 的 analysis_params 表。
.read 01_join.sql
.read 02_summary.sql
.read 03_pivot.sql
.read 04_validation.sql

SELECT * FROM validation_results;
