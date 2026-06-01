package indicator


func CalculateRSI(prices []float64, period int) float64 {
	if len(prices) < period+1 {
		return 50  
	}

	var sumGain, sumLoss float64

	for i := 1; i <= period; i++ {
		change := prices[i] - prices[i-1]
		if change >= 0 {
			sumGain += change
		} else {
			sumLoss += -change
		}
	}

	avgGain := sumGain / float64(period)
	avgLoss := sumLoss / float64(period)

	for i := period + 1; i < len(prices); i++ {
		change := prices[i] - prices[i-1]
		var gain, loss float64
		if change >= 0 {
			gain = change
		} else {
			loss = -change
		}

		avgGain = (avgGain*float64(period-1) + gain) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + loss) / float64(period)
	}

	if avgLoss == 0 {
		return 100
	}
	return 100 - 100/(1+avgGain/avgLoss)
}
