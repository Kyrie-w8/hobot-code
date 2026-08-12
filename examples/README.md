# Model deployment examples

These examples exercise Hobot Code's board-bound model deployment and acceptance contract. Generated models, datasets, credentials and toolchain output are intentionally excluded from the repository.

| Example | Purpose | Status |
|---|---|---|
| `regnet-x5` | Official torchvision RegNet-X-400MF, absent from the pinned D-Robotics X5 Model Zoo snapshot | Final end-to-end acceptance workload |
| `mobileone-x5` | Apple MobileOne-S0 classification | Regression fixture only; current D-Robotics Model Zoo already supplies MobileOne |
| `rt-igev-x5` | IGEV++ RT-IGEV stereo depth | Negative feasibility case; the monolithic graph did not meet the frozen real-time threshold |
| `x5-classification` | Small C++ `hb_dnn` latency runner shared by RGB classification examples | Runtime utility |

Every final result must be checked by `hobot deploy status`. Conversion success, model checker output or a standalone latency log is not a completed deployment.
