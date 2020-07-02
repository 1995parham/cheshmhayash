import { Component, OnInit, Input } from '@angular/core';
import { NATSService, Connz } from '../nats/nats.service';

@Component({
  selector: 'app-server',
  templateUrl: './server.component.html',
  styleUrls: ['./server.component.scss']
})
export class ServerComponent implements OnInit {

  connz: Connz;
  @Input() name: string;

  constructor(private natsService: NATSService) {  }

  ngOnInit(): void {
    this.natsService.connz(this.name).subscribe(connz => this.connz = connz);
  }

}
