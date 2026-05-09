package domain

import "container/list"

type Log struct {
	List     *list.List
	Capacity uint
	Req      chan chan<- []byte
	Write    chan<- []byte
}
