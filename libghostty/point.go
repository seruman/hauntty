package libghostty

type PointTag int

const (
	PointTagActive   PointTag = 0
	PointTagViewport PointTag = 1
	PointTagScreen   PointTag = 2
	PointTagHistory  PointTag = 3
)

type Point struct {
	Tag PointTag
	X   uint16
	Y   uint32
}
