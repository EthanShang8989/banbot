package ormu

import (
	"context"
	"strings"

	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/orm"

	"github.com/banbox/banexg/errs"
)

type FindTasksParams struct {
	Mode     string `json:"mode"`     // 可选的模式筛选
	Path     string `json:"path"`     // 可选的路径筛选
	Status   int64  `json:"status"`   // 可选的状态筛选
	Strat    string `json:"strat"`    // 可选的策略筛选（模糊匹配）
	Period   string `json:"period"`   // 可选的周期筛选（模糊匹配）
	StartAt  int64  `json:"startAt"`  // 可选的开始时间精确匹配
	StopAt   int64  `json:"stopAt"`   // 可选的结束时间精确匹配
	MinStart int64  `json:"minStart"` // 可选的最小开始时间
	MaxStart int64  `json:"maxStart"` // 可选的最大开始时间
	MaxID    int64  `json:"maxId"`    // 可选的最大ID筛选
	Limit    int64  `json:"limit"`    // 限制返回数量
}

func (q *Queries) FindTasks(ctx context.Context, arg FindTasksParams) ([]*Task, *errs.Error) {
	var b strings.Builder
	b.WriteString(`SELECT id, mode, args, config, path, strats, periods, pairs, 
		create_at, start_at, stop_at, status, progress, order_num, profit_rate, win_rate, max_drawdown, sharpe, info, note 
		FROM task WHERE 1=1 `)

	// 使用通用构建器减少重复代码
	params, paramCount := orm.BuildQuery(&b, nil, 1, []orm.IfParam{
		{Cond: arg.Mode != "", Val: arg.Mode, Tpl: "AND mode = $%d "},
		{Cond: arg.Path != "", Val: arg.Path, Tpl: "AND path = $%d "},
		{Cond: arg.Status > 0, Val: arg.Status, Tpl: "AND status = $%d "},
		{Cond: arg.Strat != "", Val: "%" + arg.Strat + "%", Tpl: "AND strats LIKE $%d "},
		{Cond: arg.Period != "", Val: "%" + arg.Period + "%", Tpl: "AND periods LIKE $%d "},
		{Cond: arg.StartAt > 0, Val: arg.StartAt, Tpl: "AND start_at = $%d "},
		{Cond: arg.StopAt > 0, Val: arg.StopAt, Tpl: "AND stop_at = $%d "},
		{Cond: arg.MinStart > 0, Val: arg.MinStart, Tpl: "AND start_at >= $%d "},
		{Cond: arg.MaxStart > 0, Val: arg.MaxStart, Tpl: "AND start_at <= $%d "},
		{Cond: arg.MaxID > 0, Val: arg.MaxID, Tpl: "AND id < $%d "},
	})

	b.WriteString("ORDER BY id DESC ")

	// LIMIT 也通过构建器添加
	params, _ = orm.BuildQuery(&b, params, paramCount, []orm.IfParam{{
		Cond: arg.Limit > 0,
		Val:  arg.Limit,
		Tpl:  "LIMIT $%d",
	}})

	// 执行查询
	rows, err := q.db.QueryContext(ctx, b.String(), params...)
	if err != nil {
		return nil, errs.New(core.ErrDbReadFail, err)
	}
	defer rows.Close()

	var items []*Task
	for rows.Next() {
		var i Task
		if err = rows.Scan(
			&i.ID,
			&i.Mode,
			&i.Args,
			&i.Config,
			&i.Path,
			&i.Strats,
			&i.Periods,
			&i.Pairs,
			&i.CreateAt,
			&i.StartAt,
			&i.StopAt,
			&i.Status,
			&i.Progress,
			&i.OrderNum,
			&i.ProfitRate,
			&i.WinRate,
			&i.MaxDrawdown,
			&i.Sharpe,
			&i.Info,
			&i.Note,
		); err != nil {
			return nil, errs.New(core.ErrDbReadFail, err)
		}
		items = append(items, &i)
	}

	if err = rows.Close(); err != nil {
		return nil, errs.New(core.ErrDbReadFail, err)
	}
	if err = rows.Err(); err != nil {
		return nil, errs.New(core.ErrDbReadFail, err)
	}

	return items, nil
}
