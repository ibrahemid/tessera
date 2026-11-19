package account

import "fmt"

func NextCounter(counter int64) (int64, error) {
	if counter < 0 || counter >= MaxCounter {
		return 0, fmt.Errorf("%w: cannot advance %d", ErrCounterRange, counter)
	}
	return counter + 1, nil
}
