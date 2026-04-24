"""
Seed script — populates the database with realistic demo data.
Run: python seed.py
"""

from database import engine, SessionLocal, create_tables
from models.db import Organization, Site, Camera, SiteSOP, User, Operator
import hashlib

def seed():
    create_tables()
    db = SessionLocal()

    # Clear existing data
    for table in [Operator, User, SiteSOP, Camera, Site, Organization]:
        db.query(table).delete()
    db.commit()

    # ── Organizations ──
    turner = Organization(
        id="co-turner", name="Turner Construction",
        plan="enterprise", contact_name="J. Vance",
        contact_email="jvance@turner.com",
    )
    bechtel = Organization(
        id="co-bechtel", name="Bechtel Corporation",
        plan="professional", contact_name="S. Miller",
        contact_email="smiller@bechtel.com",
    )
    db.add_all([turner, bechtel])
    db.flush()

    # ── Sites ──
    sites = [
        Site(
            id="TX-203", name="Southgate Power Station",
            address="4200 Industrial Blvd, Houston, TX 77001",
            organization_id="co-turner",
            latitude=29.7604, longitude=-95.3698,
            status="active",
            monitoring_start="18:00", monitoring_end="06:00",
            site_notes=[
                "Gate 3 latch is broken — stuck open until repair crew arrives Thursday",
                "Guard dog on premises after 10PM — do not dispatch to rear lot without alerting guard first",
                "Material storage yard has high-value copper inventory — priority monitoring zone",
            ],
        ),
        Site(
            id="TX-104", name="Riverside Bridge Expansion",
            address="1800 River Rd, Houston, TX 77002",
            organization_id="co-turner",
            latitude=29.7550, longitude=-95.3600,
            status="active",
            monitoring_start="19:00", monitoring_end="05:00",
            site_notes=["Bridge deck has no lighting — use IR cameras only after sunset"],
        ),
        Site(
            id="GA-091", name="Atlanta Interchange",
            address="2500 Peachtree Rd NE, Atlanta, GA 30305",
            organization_id="co-bechtel",
            latitude=33.8486, longitude=-84.3733,
            status="active",
            monitoring_start="20:00", monitoring_end="06:00",
            site_notes=["Highway patrol has jurisdiction — call 404-555-0100 before local PD"],
        ),
        Site(
            id="FL-312", name="Bayshore Medical Center",
            address="6001 Bayshore Blvd, Tampa, FL 33611",
            organization_id="co-bechtel",
            latitude=27.8850, longitude=-82.4870,
            status="active",
            monitoring_start="21:00", monitoring_end="05:00",
            site_notes=[],
        ),
    ]
    db.add_all(sites)
    db.flush()

    # ── Cameras ──
    cameras = [
        # TX-203
        Camera(id="cam-01", name="North Perimeter", site_id="TX-203", location="Gate A", status="online"),
        Camera(id="cam-02", name="Crane Zone A", site_id="TX-203", location="Tower Crane #1", status="online"),
        Camera(id="cam-03", name="Excavation Pit", site_id="TX-203", location="Foundation Level B2", status="online"),
        Camera(id="cam-04", name="South Loading", site_id="TX-203", location="Dock 3", status="online"),
        Camera(id="cam-05", name="Scaffold Tower", site_id="TX-203", location="East Wing L4", status="online"),
        Camera(id="cam-06", name="Material Storage", site_id="TX-203", location="Yard C", status="online"),
        # TX-104
        Camera(id="cam-07", name="Bridge Deck East", site_id="TX-104", location="Span 3", status="online"),
        Camera(id="cam-08", name="Bridge Deck West", site_id="TX-104", location="Span 1", status="online"),
        Camera(id="cam-09", name="Staging Area", site_id="TX-104", location="South Lot", status="online"),
        # GA-091
        Camera(id="cam-10", name="North Gate", site_id="GA-091", location="Entrance", status="online"),
        Camera(id="cam-11", name="South Ramp", site_id="GA-091", location="I-85 Exit", status="online"),
        Camera(id="cam-12", name="Equipment Yard", site_id="GA-091", location="West Lot", status="online"),
        # FL-312
        Camera(id="cam-13", name="Main Entrance", site_id="FL-312", location="Building A", status="online"),
        Camera(id="cam-14", name="Parking Deck", site_id="FL-312", location="Level 3", status="online"),
    ]
    db.add_all(cameras)
    db.flush()

    # ── SOPs ──
    sops = [
        SiteSOP(
            id="sop-001", site_id="TX-203",
            title="Person Detected — After Hours",
            category="access", priority="critical",
            steps=[
                "Review event clip — confirm human presence (not animal/debris)",
                "Switch to live view and check adjacent cameras for movement",
                "If confirmed: call on-site guard first",
                "If guard unavailable: call site supervisor R. Martinez",
                "If threat is imminent or active break-in: dispatch local PD",
                "Log all actions with timestamps in disposition notes",
                "Bookmark event clip on NVR for evidence",
            ],
            contacts=[
                {"name": "On-Site Guard", "role": "Night Security", "phone": "832-555-0100"},
                {"name": "R. Martinez", "role": "Site Supervisor", "phone": "832-555-0142", "email": "rmartinez@turner.com"},
                {"name": "Houston PD Non-Emergency", "role": "Law Enforcement", "phone": "713-884-3131"},
                {"name": "Houston PD Emergency", "role": "Emergency", "phone": "911"},
            ],
            updated_by="J. Vance",
        ),
        SiteSOP(
            id="sop-002", site_id="TX-203",
            title="Vehicle Detected — After Hours",
            category="access", priority="high",
            steps=[
                "Review event clip — identify vehicle type and license plate if visible",
                "Check if vehicle matches any scheduled deliveries or contractor vehicles",
                "Switch to live view — track vehicle movement across cameras",
                "If unauthorized: call on-site guard",
                "If vehicle is stationary near material storage: escalate to supervisor",
                "If active theft in progress: dispatch local PD immediately",
                "Capture clear screenshots of vehicle and occupants",
            ],
            contacts=[
                {"name": "On-Site Guard", "role": "Night Security", "phone": "832-555-0100"},
                {"name": "R. Martinez", "role": "Site Supervisor", "phone": "832-555-0142"},
            ],
            updated_by="S. Chen",
        ),
        SiteSOP(
            id="sop-003", site_id="TX-203",
            title="Fire / Smoke Detection",
            category="emergency", priority="critical",
            steps=[
                "Verify visual confirmation on camera — rule out steam/dust",
                "If confirmed: call 911 immediately",
                "Activate fire alarm via panel (Building C, Panel 2)",
                "Notify site supervisor and all field personnel",
                "Monitor evacuation routes on cameras 3, 5, 7",
                "Document timeline of events until fire department arrives",
            ],
            contacts=[
                {"name": "911", "role": "Emergency Services", "phone": "911"},
                {"name": "R. Martinez", "role": "Site Supervisor", "phone": "832-555-0142"},
            ],
            updated_by="J. Vance",
        ),
        SiteSOP(
            id="sop-010", site_id="GA-091",
            title="After-Hours Intrusion",
            category="access", priority="critical",
            steps=[
                "Verify motion is not wildlife/debris — check surrounding cameras",
                "If human presence confirmed, activate spotlight on zone",
                "Record 60-second clip starting 10s before event",
                "Call highway patrol desk: 404-555-0100",
                "If no response in 5 min: escalate to Bechtel duty manager",
            ],
            contacts=[
                {"name": "Highway Patrol", "role": "Law Enforcement", "phone": "404-555-0100"},
                {"name": "S. Miller", "role": "Bechtel Duty Manager", "phone": "404-555-0200"},
            ],
            updated_by="S. Miller",
        ),
    ]
    db.add_all(sops)
    db.flush()

    # ── Users ──
    pw = hashlib.sha256("demo123".encode()).hexdigest()
    users = [
        User(id="admin1", name="Admin User", email="admin@ironsight.ai", password_hash=pw, role="admin"),
        User(id="mgr1", name="J. Vance", email="jvance@turner.com", password_hash=pw, role="site_manager",
             organization_id="co-turner", assigned_site_ids=["TX-203", "TX-104"]),
        User(id="mgr2", name="S. Miller", email="smiller@bechtel.com", password_hash=pw, role="site_manager",
             organization_id="co-bechtel", assigned_site_ids=["GA-091", "FL-312"]),
    ]
    db.add_all(users)

    # ── Operators ──
    operators = [
        Operator(id="op1", name="Marcus Chen", callsign="OP-1", email="marcus@ironsight.ai", password_hash=pw, status="available"),
        Operator(id="op2", name="Sarah Rodriguez", callsign="OP-2", email="sarah@ironsight.ai", password_hash=pw, status="available"),
        Operator(id="op3", name="James Park", callsign="OP-3", email="james@ironsight.ai", password_hash=pw, status="away"),
    ]
    db.add_all(operators)

    db.commit()
    db.close()

    print("[OK] Database seeded successfully")
    print(f"  Organizations: 2 (Turner, Bechtel)")
    print(f"  Sites: 4 (TX-203, TX-104, GA-091, FL-312)")
    print(f"  Cameras: 14")
    print(f"  SOPs: 4")
    print(f"  Users: 3 (admin, 2 site managers)")
    print(f"  Operators: 3 (OP-1, OP-2, OP-3)")
    print(f"  Login: any email with password 'demo123'")


if __name__ == "__main__":
    seed()
