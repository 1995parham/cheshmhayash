import { Component, OnInit, Input } from '@angular/core';
import { NATSService, Connz, Varz } from '../nats/nats.service';

@Component({
  selector: 'app-server',
  templateUrl: './server.component.html',
  styleUrls: ['./server.component.less']
})
export class ServerComponent implements OnInit {

  connz: Connz;
  varz: Varz;
  @Input() name: string;

  constructor(private natsService: NATSService) {  }

  ngOnInit(): void {
    this.natsService.connz(this.name).subscribe(connz => this.connz = connz);
    this.natsService.varz(this.name).subscribe(varz => this.varz = varz);
  }

}
