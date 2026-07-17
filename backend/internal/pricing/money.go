package pricing

import (
	"fmt"
	"math"
	"math/bits"
)

const basisPointsOne = int64(10_000)

func pricingInteger(value any) (int64, error) {
	switch number := value.(type) {
	case int:
		return int64(number), nil
	case int64:
		return number, nil
	default:
		return 0, errorf(ErrorFactInvalid, "pricing operand has type %s", fmt.Sprintf("%T", value))
	}
}

func numeric2(operation func(int64, int64) (int64, error)) func(any, any) (int64, error) {
	return func(left, right any) (int64, error) {
		leftValue, err := pricingInteger(left)
		if err != nil {
			return 0, err
		}
		rightValue, err := pricingInteger(right)
		if err != nil {
			return 0, err
		}
		return operation(leftValue, rightValue)
	}
}

func numeric3(operation func(int64, int64, int64) (int64, error)) func(any, any, any) (int64, error) {
	return func(first, second, third any) (int64, error) {
		firstValue, err := pricingInteger(first)
		if err != nil {
			return 0, err
		}
		secondValue, err := pricingInteger(second)
		if err != nil {
			return 0, err
		}
		thirdValue, err := pricingInteger(third)
		if err != nil {
			return 0, err
		}
		return operation(firstValue, secondValue, thirdValue)
	}
}

func checkedAdd(left, right int64) (int64, error) {
	if left < 0 || right < 0 || left > math.MaxInt64-right {
		return 0, errorf(ErrorArithmeticOverflow, "integer addition overflow")
	}
	return left + right, nil
}

func checkedMulDivRoundHalfUp(left, right, divisor int64) (int64, error) {
	if left < 0 || right < 0 || divisor <= 0 {
		return 0, errorf(ErrorFactInvalid, "pricing operands must be non-negative and divisor must be positive")
	}
	hi, lo := bits.Mul64(uint64(left), uint64(right))
	d := uint64(divisor)
	if hi >= d {
		return 0, errorf(ErrorArithmeticOverflow, "integer multiplication overflow")
	}
	quotient, remainder := bits.Div64(hi, lo, d)
	if remainder >= (d+1)/2 {
		if quotient == math.MaxUint64 {
			return 0, errorf(ErrorArithmeticOverflow, "rounded amount overflow")
		}
		quotient++
	}
	if quotient > math.MaxInt64 {
		return 0, errorf(ErrorArithmeticOverflow, "amount exceeds int64")
	}
	return int64(quotient), nil
}

func tokenCost(quantity, microsPerMillion int64) (int64, error) {
	return checkedMulDivRoundHalfUp(quantity, microsPerMillion, 1_000_000)
}

func unitCost(quantity, microsPerUnit int64) (int64, error) {
	return checkedMulDivRoundHalfUp(quantity, microsPerUnit, 1)
}

func blockCost(quantity, unitsPerBlock, microsPerBlock int64) (int64, error) {
	return checkedMulDivRoundHalfUp(quantity, microsPerBlock, unitsPerBlock)
}

func multiplyBPS(amount, bps int64) (int64, error) {
	return checkedMulDivRoundHalfUp(amount, bps, basisPointsOne)
}
