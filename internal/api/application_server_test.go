package api

import (
	"context"
	"testing"

	"github.com/brocaar/lora-app-server/internal/common"
	"github.com/brocaar/lora-app-server/internal/storage"
	"github.com/brocaar/lora-app-server/internal/test"
	"github.com/brocaar/loraserver/api/as"
	"github.com/brocaar/lorawan"
	. "github.com/smartystreets/goconvey/convey"
)

func TestApplicationServerAPI(t *testing.T) {
	conf := test.GetConfig()

	Convey("Given a clean database and api instance", t, func() {
		db, err := storage.OpenDatabase(conf.PostgresDSN)
		So(err, ShouldBeNil)
		test.MustResetDB(db)

		ctx := context.Background()
		lsCtx := common.Context{DB: db}

		api := NewApplicationServerAPI(lsCtx)

		Convey("Given a node in the database", func() {
			node := storage.Node{
				Name:        "test node",
				DevEUI:      [8]byte{1, 2, 3, 4, 5, 6, 7, 8},
				AppEUI:      [8]byte{8, 7, 6, 5, 4, 3, 2, 1},
				AppKey:      [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 1, 2, 3, 4, 5, 6, 7, 8},
				RXWindow:    storage.RX2,
				RXDelay:     1,
				RX1DROffset: 2,
				RX2DR:       3,
			}

			So(storage.CreateNode(db, node), ShouldBeNil)

			phy := lorawan.PHYPayload{
				MHDR: lorawan.MHDR{
					MType: lorawan.JoinRequest,
					Major: lorawan.LoRaWANR1,
				},
				MACPayload: &lorawan.JoinRequestPayload{
					AppEUI:   node.AppEUI,
					DevEUI:   node.DevEUI,
					DevNonce: [2]byte{1, 2},
				},
			}
			So(phy.SetMIC(node.AppKey), ShouldBeNil)

			b, err := phy.MarshalBinary()
			So(err, ShouldBeNil)

			Convey("When performing a join-request", func() {

				req := as.JoinRequestRequest{
					PhyPayload: b,
					DevAddr:    []byte{1, 2, 3, 4},
					NetID:      []byte{1, 2, 3},
				}

				resp, err := api.JoinRequest(ctx, &req)
				So(err, ShouldBeNil)

				Convey("Then the expected response is returned", func() {
					node, err := storage.GetNode(db, node.DevEUI)
					So(err, ShouldBeNil)

					So(resp.NwkSKey, ShouldResemble, node.NwkSKey[:])
					So(resp.RxDelay, ShouldEqual, uint32(node.RXDelay))
					So(resp.Rx1DROffset, ShouldEqual, uint32(node.RX1DROffset))
					So(resp.CFList, ShouldHaveLength, 0)
					So(resp.RxWindow, ShouldEqual, as.RXWindow_RX2)
					So(resp.Rx2DR, ShouldEqual, uint32(node.RX2DR))

					var phy lorawan.PHYPayload
					So(phy.UnmarshalBinary(resp.PhyPayload), ShouldBeNil)

					So(phy.MHDR.MType, ShouldEqual, lorawan.JoinAccept)
					So(phy.DecryptJoinAcceptPayload(node.AppKey), ShouldBeNil)
					ok, err := phy.ValidateMIC(node.AppKey)
					So(err, ShouldBeNil)
					So(ok, ShouldBeTrue)

					jaPL, ok := phy.MACPayload.(*lorawan.JoinAcceptPayload)
					So(ok, ShouldBeTrue)

					So(jaPL.NetID, ShouldEqual, [3]byte{1, 2, 3})
					So(jaPL.DLSettings, ShouldResemble, lorawan.DLSettings{
						RX2DataRate: node.RX2DR,
						RX1DROffset: node.RX1DROffset,
					})
					So(jaPL.RXDelay, ShouldEqual, node.RXDelay)
					So(jaPL.CFList, ShouldBeNil)
					So(jaPL.DevAddr[:], ShouldResemble, []byte{1, 2, 3, 4})

					Convey("Then the DevAddr of the node has been updated", func() {
						So(node.DevAddr[:], ShouldResemble, []byte{1, 2, 3, 4})
					})
				})
			})

			Convey("Given the node as a CFList with three channels", func() {
				cl := storage.ChannelList{
					Name: "test list",
				}
				So(storage.CreateChannelList(db, &cl), ShouldBeNil)
				So(storage.CreateChannel(db, &storage.Channel{
					ChannelListID: cl.ID,
					Channel:       3,
					Frequency:     868400000,
				}), ShouldBeNil)
				So(storage.CreateChannel(db, &storage.Channel{
					ChannelListID: cl.ID,
					Channel:       4,
					Frequency:     868500000,
				}), ShouldBeNil)
				So(storage.CreateChannel(db, &storage.Channel{
					ChannelListID: cl.ID,
					Channel:       5,
					Frequency:     868600000,
				}), ShouldBeNil)

				node.ChannelListID = &cl.ID
				So(storage.UpdateNode(db, node), ShouldBeNil)

				Convey("When performing a join-request", func() {
					req := as.JoinRequestRequest{
						PhyPayload: b,
						DevAddr:    []byte{1, 2, 3, 4},
						NetID:      []byte{1, 2, 3},
					}

					resp, err := api.JoinRequest(ctx, &req)
					So(err, ShouldBeNil)

					Convey("Then the CFlist is set in the response", func() {
						node, err := storage.GetNode(db, node.DevEUI)
						So(err, ShouldBeNil)

						var phy lorawan.PHYPayload
						So(phy.UnmarshalBinary(resp.PhyPayload), ShouldBeNil)

						So(phy.DecryptJoinAcceptPayload(node.AppKey), ShouldBeNil)
						ok, err := phy.ValidateMIC(node.AppKey)
						So(err, ShouldBeNil)
						So(ok, ShouldBeTrue)

						jaPL, ok := phy.MACPayload.(*lorawan.JoinAcceptPayload)
						So(ok, ShouldBeTrue)

						So(jaPL.CFList, ShouldResemble, &lorawan.CFList{
							868400000,
							868500000,
							868600000,
							0,
							0,
						})
					})
				})
			})

			Convey("Given a pending downlink queue item for this node", func() {
				qi := storage.DownlinkQueueItem{
					DevEUI:    node.DevEUI,
					Confirmed: true,
					Pending:   true,
					FPort:     10,
					Data:      []byte{1, 2, 3, 4},
				}
				So(storage.CreateDownlinkQueueItem(db, &qi), ShouldBeNil)

				Convey("Then it is removed on a downlink ACK", func() {
					_, err := api.HandleDataDownACK(ctx, &as.HandleDataDownACKRequest{
						DevEUI: node.DevEUI[:],
					})
					So(err, ShouldBeNil)

					_, err = storage.GetDownlinkQueueItem(db, qi.ID)
					So(err, ShouldNotBeNil)
				})
			})
		})
	})
}
