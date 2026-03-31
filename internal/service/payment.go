package service

type ExternalPaymentService struct {
	ProcessPayment func(orderID string, amount float64) error
}
type PaymentService struct {
	ExternalPaymentService
}

func (s *PaymentService) ProcessPayment(orderID string, amount float64) error {
	// Placeholder for payment processing logic
	return nil
}
