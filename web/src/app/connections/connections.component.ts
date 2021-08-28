import { Component, OnInit } from '@angular/core';
import {ActivatedRoute, ParamMap} from '@angular/router';
import {NATSService, Connection, Connz} from '../nats/nats.service';
import {map, flatMap} from 'rxjs/operators';
import {Observable} from 'rxjs';
import { NzTableQueryParams } from 'ng-zorro-antd/table';


@Component({
  selector: 'app-connections',
  templateUrl: './connections.component.html',
  styleUrls: ['./connections.component.less']
})
export class ConnectionsComponent implements OnInit {

  id: Observable<string>;

  loading = true;
  pageSize = 10;
  pageIndex = 1;
  total = 0;

  connections: Connection[];

  constructor(
    private route: ActivatedRoute,
    private natsService: NATSService,
  ) { }

  ngOnInit(): void {
    this.id = this.route.paramMap.pipe(map((params: ParamMap) => params.get('id')));
    this.update();
  }

  update(): void {
    this.loading = true;
    this.id.
      pipe(flatMap((id: string) => this.natsService.connz(id, this.pageSize, this.pageSize * (this.pageIndex - 1)))).
      subscribe((connz: Connz) => {
        this.connections = connz.connections;
        this.loading = false;
        this.total = connz.total;
    });
  }

  onQueryParamsChange(params: NzTableQueryParams): void {
    console.log(params);
    const { pageSize, pageIndex } = params;

    this.pageSize = pageSize;
    this.pageIndex = pageIndex;
    this.update();
  }

}
